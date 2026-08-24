// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package commands

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/linuxfoundation/lfx-cli/internal/credstore"
	"github.com/urfave/cli/v3"
)

// newTestCommand builds a *cli.Command with the --insecure-storage and
// --backend flags registered (as newAuthLoginCommand and friends
// do), parses args against it, and returns the parsed *cli.Command handed
// to fn's Action so tests can read flag values the way the real commands
// do.
func newTestCommand(t *testing.T, args []string, fn func(cmd *cli.Command)) {
	t.Helper()
	cmd := &cli.Command{
		Name: "test",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: insecureStorageFlagName},
			&cli.StringFlag{Name: backendFlagName},
		},
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
