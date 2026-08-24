// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package credstore

import (
	"errors"
	"testing"
	"time"
)

// newTestStore builds a Store rooted at a temporary directory using the
// insecure (plain-file) backend, so tests never touch the real OS
// keychain.
func newTestStore(t *testing.T) Store {
	t.Helper()
	store, err := New(Options{Insecure: true, StateDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return store
}

func TestCredentialsRoundTrip(t *testing.T) {
	store := newTestStore(t)

	if _, err := store.LoadCredentials(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LoadCredentials before save: got err %v, want ErrNotFound", err)
	}

	want := Credentials{
		RefreshToken:      "refresh-token",
		AccessToken:       "access-token",
		AccessTokenExpiry: time.Now().Add(time.Hour).Truncate(time.Second),
	}
	if err := store.SaveCredentials(want); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	got, err := store.LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if got != want {
		t.Fatalf("LoadCredentials = %+v, want %+v", got, want)
	}

	if err := store.DeleteCredentials(); err != nil {
		t.Fatalf("DeleteCredentials: %v", err)
	}
	if _, err := store.LoadCredentials(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LoadCredentials after delete: got err %v, want ErrNotFound", err)
	}

	// DeleteCredentials is documented as a no-op when nothing is stored.
	if err := store.DeleteCredentials(); err != nil {
		t.Fatalf("DeleteCredentials on empty store: %v", err)
	}
}

func TestDeviceStateRoundTrip(t *testing.T) {
	store := newTestStore(t)

	if _, err := store.LoadDeviceState(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LoadDeviceState before save: got err %v, want ErrNotFound", err)
	}

	want := DeviceState{
		IDPDomain:   "linuxfoundation-dev.auth0.com",
		Environment: "development",
		Audience:    "https://example.test/",
		Insecure:    true,
	}
	if err := store.SaveDeviceState(want); err != nil {
		t.Fatalf("SaveDeviceState: %v", err)
	}

	got, err := store.LoadDeviceState()
	if err != nil {
		t.Fatalf("LoadDeviceState: %v", err)
	}
	if got != want {
		t.Fatalf("LoadDeviceState = %+v, want %+v", got, want)
	}

	if err := store.DeleteDeviceState(); err != nil {
		t.Fatalf("DeleteDeviceState: %v", err)
	}
	if _, err := store.LoadDeviceState(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LoadDeviceState after delete: got err %v, want ErrNotFound", err)
	}

	// DeleteDeviceState is documented as a no-op when nothing is stored.
	if err := store.DeleteDeviceState(); err != nil {
		t.Fatalf("DeleteDeviceState on empty store: %v", err)
	}
}

func TestValidAccessToken(t *testing.T) {
	tests := []struct {
		name  string
		creds Credentials
		want  bool
	}{
		{
			name:  "no access token",
			creds: Credentials{AccessTokenExpiry: time.Now().Add(time.Hour)},
			want:  false,
		},
		{
			name:  "expired",
			creds: Credentials{AccessToken: "tok", AccessTokenExpiry: time.Now().Add(-time.Minute)},
			want:  false,
		},
		{
			name:  "within clock-skew buffer of expiry",
			creds: Credentials{AccessToken: "tok", AccessTokenExpiry: time.Now().Add(10 * time.Second)},
			want:  false,
		},
		{
			name:  "valid",
			creds: Credentials{AccessToken: "tok", AccessTokenExpiry: time.Now().Add(time.Hour)},
			want:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.creds.ValidAccessToken(); got != tc.want {
				t.Errorf("ValidAccessToken() = %v, want %v", got, tc.want)
			}
		})
	}
}
