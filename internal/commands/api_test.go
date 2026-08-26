// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package commands

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfave/cli/v3"
)

// newAPITestCommand builds a *cli.Command with the `api` command's flags
// registered, parses args against it, and hands the parsed *cli.Command to
// fn, mirroring newTestCommand in auth_test.go for the auth flags.
func newAPITestCommand(t *testing.T, args []string, fn func(cmd *cli.Command)) {
	t.Helper()
	cmd := &cli.Command{
		Name:  "test",
		Flags: NewAPICommand().Flags,
		Action: func(_ context.Context, cmd *cli.Command) error {
			fn(cmd)
			return nil
		},
	}
	if err := cmd.Run(context.Background(), append([]string{"test"}, args...)); err != nil {
		t.Fatalf("cmd.Run: %v", err)
	}
}

func TestCoerceFieldValue(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  any
	}{
		{name: "true", value: "true", want: true},
		{name: "false", value: "false", want: false},
		{name: "null", value: "null", want: nil},
		{name: "integer", value: "42", want: float64(42)},
		{name: "float", value: "3.14", want: 3.14},
		{name: "plain string", value: "hello", want: "hello"},
		{name: "numeric-looking but not fully numeric", value: "42abc", want: "42abc"},
		{name: "empty string", value: "", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := coerceFieldValue(tc.value); got != tc.want {
				t.Errorf("coerceFieldValue(%q) = %#v, want %#v", tc.value, got, tc.want)
			}
		})
	}
}

func TestAPIJoinURL(t *testing.T) {
	tests := []struct {
		name string
		base string
		path string
		want string
	}{
		{name: "no trailing/leading slash", base: "https://api.example.com", path: "projects", want: "https://api.example.com/projects"},
		{name: "trailing slash on base", base: "https://api.example.com/", path: "projects", want: "https://api.example.com/projects"},
		{name: "leading slash on path", base: "https://api.example.com", path: "/projects", want: "https://api.example.com/projects"},
		{name: "both slashes", base: "https://api.example.com/", path: "/projects", want: "https://api.example.com/projects"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := apiJoinURL(tc.base, tc.path)
			if err != nil {
				t.Fatalf("apiJoinURL(%q, %q): %v", tc.base, tc.path, err)
			}
			if got != tc.want {
				t.Errorf("apiJoinURL(%q, %q) = %q, want %q", tc.base, tc.path, got, tc.want)
			}
		})
	}

	t.Run("empty base", func(t *testing.T) {
		if _, err := apiJoinURL("", "/projects"); err == nil {
			t.Fatal("apiJoinURL: got nil error, want error for empty base")
		}
	})
}

func TestAPIRequestBodyFields(t *testing.T) {
	newAPITestCommand(t, []string{
		"--field", "name=example",
		"--field", "active=true",
		"--field", "count=3",
		"--raw-field", "note=42",
	}, func(cmd *cli.Command) {
		body, contentType, err := apiRequestBody(cmd)
		if err != nil {
			t.Fatalf("apiRequestBody: %v", err)
		}
		if contentType != "application/json" {
			t.Errorf("contentType = %q, want application/json", contentType)
		}

		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("json.Unmarshal(body): %v", err)
		}
		want := map[string]any{
			"name":   "example",
			"active": true,
			"count":  float64(3),
			// --raw-field always stays a JSON string, even though "42"
			// would otherwise coerce to a number via --field.
			"note": "42",
		}
		if len(got) != len(want) {
			t.Fatalf("body = %v, want %v", got, want)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("body[%q] = %#v, want %#v", k, got[k], v)
			}
		}
	})
}

func TestAPIRequestBodyInvalidField(t *testing.T) {
	newAPITestCommand(t, []string{"--field", "no-equals-sign"}, func(cmd *cli.Command) {
		if _, _, err := apiRequestBody(cmd); err == nil {
			t.Fatal("apiRequestBody: got nil error, want error for malformed --field")
		}
	})
}

func TestAPIRequestBodyInvalidRawField(t *testing.T) {
	newAPITestCommand(t, []string{"--raw-field", "no-equals-sign"}, func(cmd *cli.Command) {
		if _, _, err := apiRequestBody(cmd); err == nil {
			t.Fatal("apiRequestBody: got nil error, want error for malformed --raw-field")
		}
	})
}

func TestAPIRequestBodyInputFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(path, []byte(`{"raw":true}`), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	newAPITestCommand(t, []string{"--input", path}, func(cmd *cli.Command) {
		body, contentType, err := apiRequestBody(cmd)
		if err != nil {
			t.Fatalf("apiRequestBody: %v", err)
		}
		if contentType != "" {
			t.Errorf("contentType = %q, want empty (raw --input sets no Content-Type)", contentType)
		}
		if string(body) != `{"raw":true}` {
			t.Errorf("body = %q, want %q", body, `{"raw":true}`)
		}
	})
}

func TestAPIRequestBodyInputRejectsCombiningWithFields(t *testing.T) {
	newAPITestCommand(t, []string{"--input", "/dev/null", "--field", "name=example"}, func(cmd *cli.Command) {
		if _, _, err := apiRequestBody(cmd); err == nil {
			t.Fatal("apiRequestBody: got nil error, want error for --input combined with --field")
		}
	})
}

func TestAPIRequestBodyNoneWithPipedStdin(t *testing.T) {
	// os.Stdin under `go test` is not a real TTY, so apiRequestBody's
	// non-char-device check treats it as piped; substitute an explicit,
	// already-closed pipe end so the test deterministically exercises
	// the "read piped stdin" branch without depending on -- or
	// blocking on -- whatever stdin the test binary happened to
	// inherit.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if _, err := w.WriteString(`{"from":"stdin"}`); err != nil {
		t.Fatalf("write to pipe: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}

	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })

	newAPITestCommand(t, nil, func(cmd *cli.Command) {
		body, contentType, err := apiRequestBody(cmd)
		if err != nil {
			t.Fatalf("apiRequestBody: %v", err)
		}
		if contentType != "" {
			t.Errorf("contentType = %q, want empty (piped stdin sets no Content-Type)", contentType)
		}
		if string(body) != `{"from":"stdin"}` {
			t.Errorf("body = %q, want %q", body, `{"from":"stdin"}`)
		}
	})
}
