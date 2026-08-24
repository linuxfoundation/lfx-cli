// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package credstore provides secure storage for LFX CLI credentials and
// non-sensitive device state.
//
// Secrets (refresh token, cached access token and its expiry) are stored in
// the operating system's credential store via github.com/99designs/keyring:
// macOS Keychain, Windows Credential Manager, Linux Secret Service/KWallet,
// or the `pass` password store. keyring's own encrypted-file backend is
// deliberately excluded from this list: it isn't a real system keychain, and
// its Remove behavior doesn't match keyring.ErrKeyNotFound (see
// keyringSecrets.Delete). If none of the allowed backends are available,
// New returns an error rather than silently falling back to a file.
//
// When --insecure-storage is passed explicitly, the system keychain is
// bypassed entirely in favor of a plain (unencrypted), owner-only (0600 on
// POSIX; on Windows, Go's Chmod maps 0600 to the read-only attribute
// instead of a real ACL, so confidentiality there relies on the file's
// inherited directory permissions) JSON file under the state directory.
// This is intended for headless/CI use where no passphrase prompt is
// acceptable, and is deliberately less secure than the keyring-backed
// storage.
//
// Non-sensitive state (environment, IdP domain, and audience used at login)
// is always stored as plain JSON under the XDG state directory
// (~/.local/state/lfx-cli/ by default), per XDG Base Directory conventions
// for mutable runtime state.
package credstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/99designs/keyring"
)

// ErrNotFound is returned by Load methods when no value has been stored yet.
var ErrNotFound = errors.New("credstore: not found")

// serviceName identifies this application's entries within shared credential
// stores (e.g. the macOS Keychain "service" attribute).
const serviceName = "lfx-cli"

// credentialsKey is the keyring item key under which Credentials are stored.
const credentialsKey = "credentials"

// stateFileName is the name of the plain-JSON file holding non-secret device
// state within the state directory.
const stateFileName = "state.json"

// insecureCredentialsFileName is the name of the plain (unencrypted) JSON
// file used to store credentials when --insecure-storage bypasses the
// system keychain.
const insecureCredentialsFileName = "credentials.json"

// systemBackends is the set of keyring backends considered real system
// credential stores. Notably excludes:
//   - keyring.FileBackend: it isn't backed by a real system keychain, and
//     its Remove behavior on a missing key doesn't match
//     keyring.ErrKeyNotFound (see keyringSecrets.Delete). Explicit
//     file-based storage is only available via --insecure-storage
//     (plainFileSecrets), never as an automatic fallback.
//   - keyring.KeyCtlBackend: its opener requires a non-empty Config.KeyCtlScope
//     ("user", "session", "process", or "thread"); with the zero value we
//     leave it at, Open always fails to open this backend and silently
//     skips it, so including it here would be a no-op.
var systemBackends = []keyring.BackendType{
	keyring.SecretServiceBackend,
	keyring.KeychainBackend,
	keyring.KWalletBackend,
	keyring.WinCredBackend,
	keyring.PassBackend,
}

// backendDisplayNames maps keyring.BackendType values to the human-readable
// names shown by `lfx auth backends`.
var backendDisplayNames = map[keyring.BackendType]string{
	keyring.SecretServiceBackend: "Secret Service (GNOME Keyring, KeePassXC, etc.)",
	keyring.KeychainBackend:      "macOS Keychain",
	keyring.KWalletBackend:       "KDE Wallet (kwallet)",
	keyring.WinCredBackend:       "Windows Credential Manager",
	keyring.PassBackend:          "gpg-encrypted vault (passwordstore.org)",
}

// Backend describes one of the system credential-store backends compiled
// into this binary for the current OS, as reported by `lfx auth backends`.
type Backend struct {
	// Name is the keyring.BackendType identifier (e.g. "keychain").
	Name string
	// DisplayName is a human-readable label for Name.
	DisplayName string
}

// AvailableBackends reports the system credential-store backends compiled
// into this binary for the current OS (Go build tags determine which
// backends are even possible per-platform; see the per-backend source
// files in github.com/99designs/keyring), in the same priority order (see
// systemBackends) that New passes to keyring.Open as AllowedBackends. It
// does not attempt to open any backend, so a backend listed here may still
// fail at login time if it isn't actually usable at runtime (e.g. no D-Bus
// session for Secret Service, `pass` not initialized, etc.).
func AvailableBackends() []Backend {
	available := make(map[keyring.BackendType]bool)
	for _, b := range keyring.AvailableBackends() {
		available[b] = true
	}

	var backends []Backend
	for _, b := range systemBackends {
		if !available[b] {
			continue
		}
		backends = append(backends, Backend{
			Name:        string(b),
			DisplayName: backendDisplayNames[b],
		})
	}
	return backends
}

// Credentials holds the secrets needed to authenticate with the LFX
// platform: the long-lived Auth0 refresh token, and an optional cached
// access token with its expiry.
type Credentials struct {
	RefreshToken      string    `json:"refresh_token"`
	AccessToken       string    `json:"access_token,omitempty"`
	AccessTokenExpiry time.Time `json:"access_token_expiry,omitempty"`
}

// ValidAccessToken reports whether the cached access token is present and
// not yet expired, allowing for a small clock-skew buffer.
func (c Credentials) ValidAccessToken() bool {
	const skew = 30 * time.Second
	return c.AccessToken != "" && time.Now().Add(skew).Before(c.AccessTokenExpiry)
}

// DeviceState holds non-sensitive information persisted between CLI
// invocations so that commands like `lfx auth token` don't need to
// re-specify the environment, IdP domain, or audience used at login.
//
// Note: this deliberately does not include a persistent "device ID". One
// was considered (see LFXV2-2515/LFXV2-2509 discussion) on the assumption
// that `gh` uses one as part of its OAuth device flow, but `gh`'s
// `~/.local/state/gh/device-id` is actually just an anonymous telemetry
// identifier (see `internal/telemetry.getOrCreateDeviceID` in
// github.com/cli/cli) -- it plays no role in the OAuth device
// authorization grant and isn't sent to GitHub's API. Since the LFX CLI
// has no telemetry pipeline, and Auth0's device flow has no concept of a
// device ID at all, there's nothing here for one to do. Revisit if/when
// opt-in CLI telemetry is added.
type DeviceState struct {
	IDPDomain string `json:"idp_domain,omitempty"`
	// Environment is the `--env` value used at login (prod, staging, or
	// development), determining which compiled-in client ID is used to
	// refresh the access token.
	Environment string `json:"environment,omitempty"`
	// Audience is the `--audience` value used at login. Auth0's
	// refresh_token grant automatically ties the refreshed access token
	// to the audience it was originally issued for, so this isn't sent
	// back on refresh; it's persisted purely for display in
	// `lfx auth status`.
	Audience string `json:"audience,omitempty"`
	// Insecure records whether `--insecure-storage` was passed at login,
	// i.e. whether Credentials live in the plain-file backend rather than
	// the system keychain. state.json itself is not namespaced by
	// backend (both share the same file), so callers must check this
	// against the invocation's own --insecure-storage flag before trusting
	// the rest of the state: without that check, logging into one backend
	// silently overwrites the metadata (env, IdP domain) that the other
	// backend's still-present credentials depend on.
	Insecure bool `json:"insecure,omitempty"`
}

// Store is the credential storage abstraction used by the auth commands.
type Store interface {
	// SaveCredentials persists secrets to the system keychain (or, with
	// Options.Insecure, the plain-file backend).
	SaveCredentials(creds Credentials) error
	// LoadCredentials returns the persisted secrets, or ErrNotFound if none
	// have been saved.
	LoadCredentials() (Credentials, error)
	// DeleteCredentials removes any persisted secrets. It is a no-op if
	// none exist.
	DeleteCredentials() error

	// SaveDeviceState persists non-sensitive device state as plain JSON.
	SaveDeviceState(state DeviceState) error
	// LoadDeviceState returns the persisted device state, or ErrNotFound if
	// none has been saved.
	LoadDeviceState() (DeviceState, error)
	// DeleteDeviceState removes any persisted device state. It is a no-op
	// if none exists.
	DeleteDeviceState() error
}

// Options configures a Store returned by New.
type Options struct {
	// Insecure bypasses the system keychain in favor of a plain, owner-only
	// (0600; see writeOwnerOnlyFile for the Windows caveat) JSON file.
	// Intended for headless/CI use where a passphrase prompt is
	// unacceptable. Corresponds to the CLI's --insecure-storage
	// flag.
	Insecure bool

	// StateDir overrides the computed state directory. Intended for tests;
	// leave empty to use $XDG_STATE_HOME/lfx-cli (or ~/.local/state/lfx-cli
	// if $XDG_STATE_HOME is unset).
	StateDir string
}

// secretsBackend abstracts over the two ways Credentials can be persisted:
// the system keychain (via keyring.Keyring), or a plain file when insecure
// storage is requested.
type secretsBackend interface {
	Save(creds Credentials) error
	Load() (Credentials, error)
	Delete() error
}

// store is the default Store implementation, backed by a secretsBackend for
// credentials and a plain JSON file for non-secret device state.
type store struct {
	secrets  secretsBackend
	stateDir string
}

// New builds a Store using the given Options.
func New(opts Options) (Store, error) {
	stateDir, err := resolveStateDir(opts.StateDir)
	if err != nil {
		return nil, fmt.Errorf("credstore: resolve state dir: %w", err)
	}

	var secrets secretsBackend
	if opts.Insecure {
		secrets = &plainFileSecrets{
			path: filepath.Join(stateDir, insecureCredentialsFileName),
		}
	} else {
		kr, err := keyring.Open(keyring.Config{
			ServiceName: serviceName,
			// The pass backend namespaces entries via PassPrefix, not
			// ServiceName; without it, our generic credentialsKey would be
			// stored as a top-level "credentials" entry in the user's
			// password store, risking collisions with unrelated tools.
			PassPrefix:               serviceName,
			AllowedBackends:          systemBackends,
			KeychainTrustApplication: true,
		})
		if err != nil {
			return nil, fmt.Errorf("credstore: open keyring (use --insecure-storage as a fallback): %w", err)
		}
		secrets = &keyringSecrets{keyring: kr}
	}

	return &store{secrets: secrets, stateDir: stateDir}, nil
}

// resolveStateDir determines the directory used for non-secret device state
// and the insecure-storage credentials file, creating it if necessary.
func resolveStateDir(override string) (string, error) {
	dir := override
	if dir == "" {
		// XDG Base Directory Specification requires $XDG_STATE_HOME to be an
		// absolute path; per spec, relative values (and thus the variable)
		// must be ignored, falling back to the documented default.
		if xdgState := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(xdgState) {
			dir = filepath.Join(xdgState, "lfx-cli")
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("determine home directory: %w", err)
			}
			dir = filepath.Join(home, ".local", "state", "lfx-cli")
		}
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create state directory %q: %w", dir, err)
	}

	return dir, nil
}

// writeOwnerOnlyFile writes data to path in place, creating it with
// owner-only (0600) permissions if it doesn't already exist. path is
// exclusively created and managed by this CLI, so there's no other writer
// to race against and no need for a temp-file-plus-rename swap: that would
// only introduce problems here, since os.Rename isn't guaranteed atomic on
// Windows, and on SELinux systems a renamed-in file can pick up the temp
// file's security context instead of the destination's. The accepted
// tradeoff is that a crash or write error between truncation and the write
// completing can leave the file empty or partially written, requiring the
// user to re-authenticate; for a local single-writer state file, that's a
// simpler and more predictable failure mode than the complexity a
// crash-safe rename would add.
//
// The 0600 mode is requested only at creation time (per os.OpenFile
// semantics); if path already exists with looser permissions (e.g. a user
// deliberately loosened them), this does not tighten them back. It's the
// user's file to manage once it exists.
//
// On Windows, 0600 does not enforce owner-only access: Go maps it to the
// read-only attribute rather than a real ACL, so confidentiality there
// depends on the file's inherited directory permissions.
func writeOwnerOnlyFile(path string, data []byte) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()

	if _, err = f.Write(data); err != nil {
		return err
	}

	return f.Sync()
}

// SaveCredentials implements Store.
func (s *store) SaveCredentials(creds Credentials) error {
	return s.secrets.Save(creds)
}

// LoadCredentials implements Store.
func (s *store) LoadCredentials() (Credentials, error) {
	return s.secrets.Load()
}

// DeleteCredentials implements Store.
func (s *store) DeleteCredentials() error {
	return s.secrets.Delete()
}

// SaveDeviceState implements Store.
func (s *store) SaveDeviceState(state DeviceState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("credstore: marshal device state: %w", err)
	}

	path := filepath.Join(s.stateDir, stateFileName)
	if err := writeOwnerOnlyFile(path, data); err != nil {
		return fmt.Errorf("credstore: write device state: %w", err)
	}

	return nil
}

// LoadDeviceState implements Store.
func (s *store) LoadDeviceState() (DeviceState, error) {
	path := filepath.Join(s.stateDir, stateFileName)

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DeviceState{}, ErrNotFound
	}
	if err != nil {
		return DeviceState{}, fmt.Errorf("credstore: read device state: %w", err)
	}

	var state DeviceState
	if err := json.Unmarshal(data, &state); err != nil {
		return DeviceState{}, fmt.Errorf("credstore: unmarshal device state: %w", err)
	}

	return state, nil
}

// DeleteDeviceState implements Store.
func (s *store) DeleteDeviceState() error {
	path := filepath.Join(s.stateDir, stateFileName)

	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("credstore: delete device state: %w", err)
	}

	return nil
}

// keyringSecrets is a secretsBackend that stores Credentials in a real
// system credential store via keyring.Keyring (see systemBackends).
type keyringSecrets struct {
	keyring keyring.Keyring
}

func (k *keyringSecrets) Save(creds Credentials) error {
	data, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("credstore: marshal credentials: %w", err)
	}

	err = k.keyring.Set(keyring.Item{
		Key:         credentialsKey,
		Data:        data,
		Label:       "LFX CLI credentials",
		Description: "Refresh and access tokens for the LFX platform",
	})
	if err != nil {
		return fmt.Errorf("credstore: save credentials: %w", err)
	}

	return nil
}

func (k *keyringSecrets) Load() (Credentials, error) {
	item, err := k.keyring.Get(credentialsKey)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return Credentials{}, ErrNotFound
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("credstore: load credentials: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(item.Data, &creds); err != nil {
		return Credentials{}, fmt.Errorf("credstore: unmarshal credentials: %w", err)
	}

	return creds, nil
}

func (k *keyringSecrets) Delete() error {
	err := k.keyring.Remove(credentialsKey)
	// Most backends return keyring.ErrKeyNotFound for a missing key, but
	// some (e.g. the pass backend, which shells out to files on disk) may
	// instead surface an os.ErrNotExist-wrapping error; treat both as a
	// successful no-op per the Store.DeleteCredentials contract.
	if err != nil && !errors.Is(err, keyring.ErrKeyNotFound) && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("credstore: delete credentials: %w", err)
	}

	return nil
}

// plainFileSecrets is a secretsBackend that stores Credentials as a plain
// (unencrypted), owner-only (0600) JSON file. Used when --insecure-storage
// is passed, bypassing the system keychain entirely so that no passphrase
// prompt is ever required (e.g. for headless/CI use). This is deliberately
// less secure than keyringSecrets.
type plainFileSecrets struct {
	path string
}

func (p *plainFileSecrets) Save(creds Credentials) error {
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("credstore: marshal credentials: %w", err)
	}

	if err := writeOwnerOnlyFile(p.path, data); err != nil {
		return fmt.Errorf("credstore: write credentials: %w", err)
	}

	return nil
}

func (p *plainFileSecrets) Load() (Credentials, error) {
	data, err := os.ReadFile(p.path)
	if errors.Is(err, os.ErrNotExist) {
		return Credentials{}, ErrNotFound
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("credstore: read credentials: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return Credentials{}, fmt.Errorf("credstore: unmarshal credentials: %w", err)
	}

	return creds, nil
}

func (p *plainFileSecrets) Delete() error {
	err := os.Remove(p.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("credstore: delete credentials: %w", err)
	}

	return nil
}
