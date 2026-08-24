// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package commands implements the lfx CLI subcommands.
package commands

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/linuxfoundation/lfx-cli/internal/credstore"
	"github.com/urfave/cli/v3"
	"golang.org/x/oauth2"
)

// insecureStorageFlagName is the auth command group's flag controlling
// whether credentials bypass the system keychain in favor of a plain,
// unencrypted file.
const insecureStorageFlagName = "insecure-storage"

// backendFlagName is the auth command group's flag pinning
// credential storage to a single system keyring backend (see
// `lfx auth backends`), instead of letting keyring.Open silently pick
// whichever backend currently opens. Ignored when --insecure-storage is
// set.
const backendFlagName = "backend"

// Flag names shared by the login command.
const (
	webFlagName       = "web"
	withTokenFlagName = "with-token"
	envFlagName       = "env"
	audienceFlagName  = "audience"
)

// scopes requested during the device code flow. offline_access is required
// to receive a refresh token; the rest identify the user for `auth status`.
var loginScopes = []string{"openid", "profile", "email", "offline_access"}

// NewAuthCommand builds the `lfx auth` command group with its subcommands.
//
// The --insecure-storage and --backend flags are shared by all
// subcommands (they are not declared as "Local" flags, so urfave/cli
// resolves them for subcommand actions via cmd.Bool/cmd.String).
// --insecure-storage controls whether credentials bypass the system
// backend in favor of credstore's plain (unencrypted) file fallback, e.g.
// for headless/CI use. --backend pins credential storage to a
// single system backend rather than letting keyring.Open silently pick
// whichever one currently opens; see credstore.DeviceState.Backend for why
// that matters once a login has pinned one.
func NewAuthCommand() *cli.Command {
	return &cli.Command{
		Name:  "auth",
		Usage: "Manage authentication with the LFX platform",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  insecureStorageFlagName,
				Usage: "Store credentials in a plain (unencrypted) file instead of the system backend",
			},
			&cli.StringFlag{
				Name:  backendFlagName,
				Usage: "Pin credential storage to a specific system backend (see `lfx auth backends`); ignored with --insecure-storage",
			},
		},
		Commands: []*cli.Command{
			newAuthLoginCommand(),
			newAuthTokenCommand(),
			newAuthStatusCommand(),
			newAuthLogoutCommand(),
			newAuthBackendsCommand(),
		},
	}
}

// credStoreFromCommand builds a credstore.Store using the group-level
// --insecure-storage and --backend flags, however deep in the
// `auth` subcommand tree cmd is.
func credStoreFromCommand(cmd *cli.Command) (credstore.Store, error) {
	insecure := cmd.Bool(insecureStorageFlagName)
	backend := cmd.String(backendFlagName)
	if insecure && backend != "" {
		return nil, fmt.Errorf("--%s cannot be combined with --%s", backendFlagName, insecureStorageFlagName)
	}
	return credstore.New(credstore.Options{Insecure: insecure, Backend: backend})
}

func newAuthLoginCommand() *cli.Command {
	return &cli.Command{
		Name:  "login",
		Usage: "Log in to the LFX platform via the Auth0 Device Code flow",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    webFlagName,
				Aliases: []string{"w"},
				Usage:   "Automatically open the verification URL in the default browser",
			},
			&cli.BoolFlag{
				Name:  withTokenFlagName,
				Usage: "Read a refresh token from stdin instead of performing the interactive Device Code flow",
			},
			&cli.StringFlag{
				Name:  envFlagName,
				Usage: "Target environment: prod, staging, or development",
				Value: string(envProd),
			},
			&cli.StringFlag{
				Name:  audienceFlagName,
				Usage: "Auth0 API audience to request tokens for (independent of --env)",
				Value: defaultAudience,
			},
		},
		Action: runAuthLogin,
	}
}

func runAuthLogin(ctx context.Context, cmd *cli.Command) error {
	store, err := credStoreFromCommand(cmd)
	if err != nil {
		return err
	}

	env := authEnvironment(cmd.String(envFlagName))
	domain, clientID, err := resolveEnvironment(env)
	if err != nil {
		return err
	}
	audience := cmd.String(audienceFlagName)
	insecure := cmd.Bool(insecureStorageFlagName)
	backend := cmd.String(backendFlagName)

	if cmd.Bool(withTokenFlagName) {
		return loginWithToken(store, env, domain, audience, insecure, backend)
	}

	return loginWithDeviceCode(ctx, cmd, domain, clientID, store, env, audience, insecure, backend)
}

// loginWithToken implements `--with-token`: it reads a refresh token from
// stdin (one line, trimmed) for headless/CI use, e.g.
// `echo "$REFRESH_TOKEN" | lfx auth login --with-token`. No access token is
// cached; the next `lfx auth token` call exchanges the refresh token for
// one.
func loginWithToken(store credstore.Store, env authEnvironment, domain, audience string, insecure bool, backend string) error {
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read refresh token from stdin: %w", err)
	}
	refreshToken := strings.TrimSpace(line)
	if refreshToken == "" {
		return errors.New("no refresh token provided on stdin")
	}

	if err := persistLogin(
		store,
		credstore.Credentials{RefreshToken: refreshToken},
		credstore.DeviceState{IDPDomain: domain, Environment: string(env), Audience: audience, Insecure: insecure, Backend: backend},
	); err != nil {
		return err
	}

	fmt.Println("Logged in with a supplied refresh token.")
	return nil
}

// loginWithDeviceCode performs the interactive Auth0 Device Code flow:
// request a device code, show the user code and verification URL
// (optionally opening it in a browser), then poll until the user completes
// or the flow expires/is denied.
func loginWithDeviceCode(
	ctx context.Context,
	cmd *cli.Command,
	domain, clientID string,
	store credstore.Store,
	env authEnvironment,
	audience string,
	insecure bool,
	backend string,
) error {
	cfg := &oauth2.Config{
		ClientID: clientID,
		Scopes:   loginScopes,
		Endpoint: oauth2.Endpoint{
			DeviceAuthURL: "https://" + domain + "/oauth/device/code",
			TokenURL:      "https://" + domain + "/oauth/token",
			AuthStyle:     oauth2.AuthStyleInParams,
		},
	}
	var opts []oauth2.AuthCodeOption
	if audience != "" {
		opts = append(opts, oauth2.SetAuthURLParam("audience", audience))
	}
	resp, err := cfg.DeviceAuth(ctx, opts...)
	if err != nil {
		return fmt.Errorf("request device code: %w", err)
	}

	fmt.Printf("First copy your one-time code: %s\n", resp.UserCode)
	// VerificationURIComplete is optional per RFC 8628 §3.2; fall back to
	// VerificationURI (always present) plus the user code if the IdP
	// doesn't supply it.
	verificationURI := resp.VerificationURIComplete
	if verificationURI == "" {
		verificationURI = resp.VerificationURI
	}
	if cmd.Bool(webFlagName) {
		fmt.Printf("Opening %s in your browser...\n", verificationURI)
		if err := openBrowser(verificationURI); err != nil {
			fmt.Printf("Couldn't open browser automatically: %v\n", err)
			fmt.Printf("Please visit: %s\n", verificationURI)
		}
	} else {
		fmt.Printf("Then visit: %s\n", verificationURI)
	}
	fmt.Println("Waiting for authentication...")

	// DeviceAccessToken blocks until the user completes or rejects the
	// flow, polling at the interval Auth0 specified (honoring "slow_down"
	// backoff), bounded by the device code's own expiry.
	token, err := cfg.DeviceAccessToken(ctx, resp)
	if err != nil {
		// DeviceAccessToken derives its polling deadline from the device
		// code's expiry (RFC 8628 "expires_in") and, once that elapses,
		// returns a bare context.DeadlineExceeded instead of waiting for
		// one more poll where the server would otherwise answer
		// error=expired_token itself. Both mean the same thing -- the
		// device code's verification window ran out, not a token or
		// network timeout -- so both map to the same message.
		var retrieveErr *oauth2.RetrieveError
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return errors.New("timed out waiting for login")
		case errors.As(err, &retrieveErr) && retrieveErr.ErrorCode == "expired_token":
			return errors.New("timed out waiting for login")
		case errors.As(err, &retrieveErr) && retrieveErr.ErrorCode == "access_denied":
			return errors.New("login was denied")
		default:
			return fmt.Errorf("unexpected error waiting for login: %w", err)
		}
	}

	// offline_access was requested (see loginScopes), but Auth0 does not
	// guarantee a refresh_token is issued -- e.g. a custom API/audience
	// can have offline access disabled server-side regardless of the
	// scopes requested. Persisting an empty refresh token here would let
	// `lfx auth login` report success for a session that silently stops
	// working (or worse, is treated as logged-in with no way to refresh)
	// as soon as the cached access token expires, so fail loudly instead.
	if token.RefreshToken == "" {
		return errors.New("login did not receive a refresh token; offline access may be disabled for this API")
	}

	if err := persistLogin(
		store,
		credstore.Credentials{
			RefreshToken:      token.RefreshToken,
			AccessToken:       token.AccessToken,
			AccessTokenExpiry: token.Expiry,
		},
		credstore.DeviceState{IDPDomain: domain, Environment: string(env), Audience: audience, Insecure: insecure, Backend: backend},
	); err != nil {
		return err
	}

	fmt.Println("Login successful.")
	if idToken, ok := token.Extra("id_token").(string); ok {
		if identity := identityFromIDToken(idToken); identity != "" {
			fmt.Printf("Logged in as %s.\n", identity)
		}
	}
	return nil
}

// loadDeviceStateForBackend loads the persisted device state and validates
// it against the current invocation before returning it:
//
//   - state.Insecure must match cmd's --insecure-storage flag. state.json is
//     shared by both the keychain and plain-file credential backends (see
//     credstore.DeviceState.Insecure), so a mismatch means this invocation's
//     credentials were saved under a different backend than the one that
//     last wrote state.json -- trusting it here would silently mix a
//     refresh token from one backend with IdP/environment metadata written
//     for the other.
//   - if state.Backend was pinned (non-empty; see
//     credstore.DeviceState.Backend), it must match cmd's --backend
//     flag. Unlike Insecure, an unpinned state.Backend ("") can't be
//     checked at all: keyring.Open can silently land on a different system
//     backend across invocations, and once it does there's no recorded
//     value to compare against.
//   - the IdP domain implied by state.Environment (via resolveEnvironment,
//     the source of truth) must match the persisted state.IDPDomain,
//     guarding against a tampered or corrupted state.json redirecting a
//     refresh request -- and its long-lived refresh token -- to another
//     host.
//
// On success it returns the state along with the trusted domain and client
// ID to use for any Auth0 request (always resolveEnvironment's values,
// never the persisted ones).
func loadDeviceStateForBackend(store credstore.Store, cmd *cli.Command) (state credstore.DeviceState, domain, clientID string, err error) {
	state, err = store.LoadDeviceState()
	if err != nil {
		return credstore.DeviceState{}, "", "", err
	}

	if state.Insecure != cmd.Bool(insecureStorageFlagName) {
		return credstore.DeviceState{}, "", "", fmt.Errorf(
			"stored login state belongs to %s; pass %s to match, or run `lfx auth login` again",
			backendDescription(state.Insecure), insecureStorageUsageHint(state.Insecure),
		)
	}
	if state.Backend != "" && state.Backend != cmd.String(backendFlagName) {
		return credstore.DeviceState{}, "", "", fmt.Errorf(
			"stored login was pinned to keyring backend %q; pass --%s=%s to match, or run `lfx auth login --%s=%s` again",
			state.Backend, backendFlagName, state.Backend, backendFlagName, state.Backend,
		)
	}

	domain, clientID, err = resolveEnvironment(authEnvironment(state.Environment))
	if err != nil {
		return credstore.DeviceState{}, "", "", err
	}
	if domain != state.IDPDomain {
		return credstore.DeviceState{}, "", "", fmt.Errorf(
			"stored IdP domain %q does not match %q for environment %q; run `lfx auth login` again",
			state.IDPDomain, domain, state.Environment,
		)
	}

	return state, domain, clientID, nil
}

// backendDescription renders a human-readable name for a credential
// backend, for use in error messages.
func backendDescription(insecure bool) string {
	if insecure {
		return "the plain-file (--insecure-storage) backend"
	}
	return "the system backend"
}

// insecureStorageUsageHint renders the flag (or its absence) needed to
// select the given backend, for use in error messages.
func insecureStorageUsageHint(insecure bool) string {
	if insecure {
		return "--insecure-storage"
	}
	return "no --insecure-storage"
}

// stateMatchesInvocation reports whether state was written by an
// invocation using the same --insecure-storage and (if pinned)
// --backend as cmd, i.e. whether it's safe to trust or overwrite
// state.json for this invocation. See loadDeviceStateForBackend for the
// same checks used when actually consuming the state.
func stateMatchesInvocation(state credstore.DeviceState, cmd *cli.Command) bool {
	if state.Insecure != cmd.Bool(insecureStorageFlagName) {
		return false
	}
	return state.Backend == "" || state.Backend == cmd.String(backendFlagName)
}

// persistLogin saves creds and state as a pair. The two writes are not
// atomic: if SaveDeviceState fails after SaveCredentials succeeds, the
// just-saved credentials are rolled back (deleted) rather than left paired
// with stale or missing environment metadata, which could otherwise send a
// later refresh to the wrong Auth0 tenant.
//
// This rollback is a delete, not a restore: on a re-login (the user was
// already authenticated), SaveCredentials has already overwritten the
// prior working credentials before SaveDeviceState is attempted, so a
// SaveDeviceState failure here logs the user out rather than leaving the
// previous, still-valid session in place. This is considered acceptable:
// the failure mode requires state.json's write to fail (e.g. disk full or
// permissions) immediately after a credentials write succeeded, and the
// fix -- snapshotting and restoring prior credentials instead of deleting
// -- adds meaningful complexity for a narrow, easily-recovered case (rerun
// `lfx auth login`).
func persistLogin(store credstore.Store, creds credstore.Credentials, state credstore.DeviceState) error {
	if err := store.SaveCredentials(creds); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	if err := store.SaveDeviceState(state); err != nil {
		if delErr := store.DeleteCredentials(); delErr != nil {
			return fmt.Errorf("save device state: %w (and rollback of saved credentials also failed: %v)", err, delErr)
		}
		return fmt.Errorf("save device state: %w", err)
	}
	return nil
}

// loadStoredCredentials builds a credstore.Store for cmd and loads its
// credentials, returning (creds, false, nil) when none are stored
// (credstore.ErrNotFound) instead of treating that as an error, since
// callers report "not logged in" differently.
func loadStoredCredentials(cmd *cli.Command) (store credstore.Store, creds credstore.Credentials, found bool, err error) {
	store, err = credStoreFromCommand(cmd)
	if err != nil {
		return nil, credstore.Credentials{}, false, err
	}
	creds, err = store.LoadCredentials()
	if errors.Is(err, credstore.ErrNotFound) {
		return store, credstore.Credentials{}, false, nil
	}
	if err != nil {
		return nil, credstore.Credentials{}, false, err
	}
	return store, creds, true, nil
}

func newAuthTokenCommand() *cli.Command {
	return &cli.Command{
		Name:  "token",
		Usage: "Print a valid access token for the LFX platform",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			store, creds, found, err := loadStoredCredentials(cmd)
			if err != nil {
				return err
			}
			if !found {
				return errors.New("not logged in; run `lfx auth login` first")
			}

			if creds.ValidAccessToken() {
				fmt.Println(creds.AccessToken)
				return nil
			}

			if creds.RefreshToken == "" {
				return errors.New("no refresh token available; run `lfx auth login` again")
			}

			_, domain, clientID, err := loadDeviceStateForBackend(store, cmd)
			if err != nil {
				return fmt.Errorf("load device state: %w", err)
			}
			cfg := &oauth2.Config{
				ClientID: clientID,
				Endpoint: oauth2.Endpoint{
					DeviceAuthURL: "https://" + domain + "/oauth/device/code",
					TokenURL:      "https://" + domain + "/oauth/token",
					AuthStyle:     oauth2.AuthStyleInParams,
				},
			}

			token, err := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: creds.RefreshToken}).Token()
			var retrieveErr *oauth2.RetrieveError
			if errors.As(err, &retrieveErr) && retrieveErr.ErrorCode == "invalid_grant" {
				return errors.New("session expired or revoked; run `lfx auth login` to log in again")
			}
			if err != nil {
				return fmt.Errorf("refresh access token: %w", err)
			}

			refreshToken := token.RefreshToken
			if refreshToken == "" {
				// Auth0 may not rotate the refresh token on every
				// exchange; keep the existing one in that case.
				refreshToken = creds.RefreshToken
			}
			if err := store.SaveCredentials(credstore.Credentials{
				RefreshToken:      refreshToken,
				AccessToken:       token.AccessToken,
				AccessTokenExpiry: token.Expiry,
			}); err != nil {
				return fmt.Errorf("save refreshed credentials: %w", err)
			}

			fmt.Println(token.AccessToken)
			return nil
		},
	}
}

func newAuthStatusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Show the current authentication status",
		Action: func(_ context.Context, cmd *cli.Command) error {
			store, creds, found, err := loadStoredCredentials(cmd)
			if err != nil {
				return err
			}
			if !found {
				fmt.Println("Not logged in.")
				return nil
			}

			state, err := store.LoadDeviceState()
			if err != nil && !errors.Is(err, credstore.ErrNotFound) {
				return fmt.Errorf("load device state: %w", err)
			}

			backend := "system backend"
			if cmd.Bool(insecureStorageFlagName) {
				backend = "plain file (--insecure-storage)"
			}

			fmt.Println("Logged in.")
			if err == nil && !stateMatchesInvocation(state, cmd) {
				fmt.Printf(
					"  Note: stored login state below belongs to %s, not this credential backend; "+
						"it may not describe these credentials. Run `lfx auth login` to refresh it.\n",
					backendDescription(state.Insecure),
				)
			}
			if state.Environment != "" {
				fmt.Printf("  %-22s %s\n", "Environment:", state.Environment)
			}
			if state.IDPDomain != "" {
				fmt.Printf("  %-22s %s\n", "IdP domain:", state.IDPDomain)
			}
			if state.Audience != "" {
				fmt.Printf("  %-22s %s\n", "Audience:", state.Audience)
			}
			fmt.Printf("  %-22s %s\n", "Credential backend:", backend)
			if state.Backend != "" {
				fmt.Printf("  %-22s %s\n", "Pinned backend:", state.Backend)
			}
			if creds.ValidAccessToken() {
				fmt.Printf("  %-22s %s\n", "Access token expires:", creds.AccessTokenExpiry.Format(time.RFC3339))
			} else {
				fmt.Printf("  %-22s %s\n", "Access token:", "expired or not cached (will refresh on next `lfx auth token`)")
			}

			return nil
		},
	}
}

func newAuthLogoutCommand() *cli.Command {
	return &cli.Command{
		Name:  "logout",
		Usage: "Remove stored LFX platform credentials",
		Action: func(_ context.Context, cmd *cli.Command) error {
			store, err := credStoreFromCommand(cmd)
			if err != nil {
				return err
			}

			if err := store.DeleteCredentials(); err != nil {
				return fmt.Errorf("delete credentials: %w", err)
			}

			// state.json is shared by both credential backends (see
			// credstore.DeviceState.Insecure), so only delete it when it
			// actually describes this invocation's backend; otherwise
			// logging out of one backend would destroy metadata (env, IdP
			// domain) still needed by the other backend's credentials.
			state, err := store.LoadDeviceState()
			switch {
			case errors.Is(err, credstore.ErrNotFound):
				// Nothing to delete.
			case err != nil:
				return fmt.Errorf("load device state: %w", err)
			case stateMatchesInvocation(state, cmd):
				if err := store.DeleteDeviceState(); err != nil {
					return fmt.Errorf("delete device state: %w", err)
				}
			default:
				fmt.Printf(
					"Note: leaving stored login state in place; it belongs to %s.\n",
					backendDescription(state.Insecure),
				)
			}

			fmt.Println("Logged out.")
			return nil
		},
	}
}

// newAuthBackendsCommand builds `lfx auth backends`, which lists the system
// credential-store backends compiled into this binary for the current OS
// (see credstore.AvailableBackends), in the priority order `lfx auth login`
// would try them. It does not attempt to open any backend, so a listed
// backend may still turn out to be unusable at runtime (e.g. no D-Bus
// session for Secret Service, `pass` not initialized).
func newAuthBackendsCommand() *cli.Command {
	return &cli.Command{
		Name:  "backends",
		Usage: "List the system credential-store backends available on this OS",
		Action: func(_ context.Context, _ *cli.Command) error {
			backends := credstore.AvailableBackends()
			if len(backends) == 0 {
				fmt.Println("No system credential-store backends are available on this OS; `lfx auth login` requires --insecure-storage.")
				return nil
			}

			fmt.Println("Available credential-store backends, in the order `lfx auth login` tries them:")
			for _, b := range backends {
				fmt.Printf("  %-15s %s\n", b.Name, b.DisplayName)
			}
			return nil
		},
	}
}

// lfidClaimsNamespace prefixes the custom LFID claims Auth0 adds to the ID
// token, namely username. Distinct from the shorter "http://lfx.dev/claims"
// LFX claims namespace used elsewhere.
const lfidClaimsNamespace = "https://sso.linuxfoundation.org/claims/"

// identityFromIDToken extracts a human-readable identity from an unverified
// decode of the ID token's JWT payload, for a friendly "Logged in as ..."
// message; the access token (verified server-side on every API call) is the
// actual credential. LFX usernames (the custom claim above) are the
// conventional identifier; email is included alongside it when present, and
// email or subject alone are used as fallbacks if the custom claim is
// unexpectedly missing.
func identityFromIDToken(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return ""
	}

	payload, err := base64URLDecode(parts[1])
	if err != nil {
		return ""
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}

	username, _ := claims[lfidClaimsNamespace+"username"].(string)
	email, _ := claims["email"].(string)
	sub, _ := claims["sub"].(string)

	switch {
	case username != "" && email != "":
		return fmt.Sprintf("%s (%s)", username, email)
	case username != "":
		return username
	case email != "":
		return fmt.Sprintf("%s (no username)", email)
	default:
		return sub
	}
}

// base64URLDecode decodes a base64url-encoded JWT segment, tolerating the
// missing padding that JWTs conventionally omit.
func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// openBrowser opens url in the user's default browser, following the
// per-OS conventions used by tools like `gh`.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
