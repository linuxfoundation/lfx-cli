// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package commands

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// goaTestNamedError is a stand-in for a Goa-generated typed error: Error
// returns "" and the detail is in a Message field, matching goaNamedError.
type goaTestNamedError struct {
	Name    string
	Message string
}

func (e *goaTestNamedError) Error() string        { return "" }
func (e *goaTestNamedError) GoaErrorName() string { return e.Name }

// TestFormatGoaError covers formatGoaError's handling of Goa's typed
// errors (name and message from reflection) alongside ordinary errors.
func TestFormatGoaError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "named error with message",
			err:  &goaTestNamedError{Name: "not_found", Message: "project does not exist"},
			want: "not_found: project does not exist",
		},
		{
			name: "named error without message",
			err:  &goaTestNamedError{Name: "internal"},
			want: "internal",
		},
		{
			name: "ordinary error passes through",
			err:  errors.New("boom"),
			want: "boom",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatGoaError(tc.err)
			if got == nil || got.Error() != tc.want {
				t.Fatalf("formatGoaError(%v) = %v, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestRawBodyDoer checks that rawBodyDoer keeps a readable copy of the
// response body while leaving resp.Body itself readable by the caller.
func TestRawBodyDoer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	doer := &rawBodyDoer{inner: http.DefaultClient}
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := doer.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if string(doer.body) != `{"ok":true}` {
		t.Errorf("doer.body = %q, want %q", doer.body, `{"ok":true}`)
	}
	again, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read resp.Body: %v", err)
	}
	if string(again) != `{"ok":true}` {
		t.Errorf("resp.Body = %q, want %q", again, `{"ok":true}`)
	}
}
