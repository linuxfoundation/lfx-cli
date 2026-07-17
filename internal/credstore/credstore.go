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
// bypassed entirely in favor of a plain (unencrypted), owner-only (0600)
// JSON file under the state directory. This is intended for headless/CI use
// where no passphrase prompt is acceptable, and is deliberately less secure
// than the keyring-backed storage.
//
// Non-sensitive state (device ID, IdP domain used at login) is always stored
// as plain JSON under the XDG state directory (~/.local/state/lfx-cli/ by
// default), per XDG Base Directory conventions for mutable runtime state.
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
// credential stores. Notably excludes keyring.FileBackend: it isn't backed
// by a real system keychain, and its Remove behavior on a missing key
// doesn't match keyring.ErrKeyNotFound (see keyringSecrets.Delete). Explicit
// file-based storage is only available via --insecure-storage
// (plainFileSecrets), never as an automatic fallback.
var systemBackends = []keyring.BackendType{
	keyring.SecretServiceBackend,
	keyring.KeychainBackend,
	keyring.KeyCtlBackend,
	keyring.KWalletBackend,
	keyring.WinCredBackend,
	keyring.PassBackend,
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
// re-specify the IdP domain used at login.
type DeviceState struct {
	DeviceID  string `json:"device_id"`
	IDPDomain string `json:"idp_domain,omitempty"`
}

// Store is the credential storage abstraction used by the auth commands.
type Store interface {
	// SaveCredentials persists secrets to the system keychain (or the file
	// fallback).
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
}

// Options configures a Store returned by New.
type Options struct {
	// Insecure bypasses the system keychain (and its encrypted-file
	// fallback) in favor of a plain, owner-only (0600) JSON file. Intended
	// for headless/CI use where a passphrase prompt is unacceptable.
	// Corresponds to the CLI's --insecure-storage flag.
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
			ServiceName:              serviceName,
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
	if err := os.WriteFile(path, data, 0o600); err != nil {
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

	if err := os.WriteFile(p.path, data, 0o600); err != nil {
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
