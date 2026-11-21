// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/linuxfoundation/lfx-cli/internal/credstore"
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
		{name: "integer", value: "42", want: json.Number("42")},
		{name: "float", value: "3.14", want: json.Number("3.14")},
		{name: "plain string", value: "hello", want: "hello"},
		{name: "numeric-looking but not fully numeric", value: "42abc", want: "42abc"},
		{name: "empty string", value: "", want: ""},
		{name: "NaN stays a string", value: "NaN", want: "NaN"},
		{name: "Infinity stays a string", value: "Infinity", want: "Infinity"},
		{name: "hex float stays a string", value: "0x1p2", want: "0x1p2"},
		{name: "leading zero stays a string (not valid JSON number)", value: "042", want: "042"},
		{name: "large integer preserves precision", value: "9007199254740993", want: json.Number("9007199254740993")},
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
		{name: "query string preserved", base: "https://api.example.com", path: "/my-grants?v=1&object_type=project", want: "https://api.example.com/my-grants?v=1&object_type=project"},
		{name: "fragment preserved", base: "https://api.example.com", path: "/projects#frag", want: "https://api.example.com/projects#frag"},
		{name: "encoded slash preserved verbatim", base: "https://api.example.com", path: "/objects/a%2Fb", want: "https://api.example.com/objects/a%2Fb"},
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

func TestAPIRequireHTTPS(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{name: "https", rawURL: "https://api.example.com", wantErr: false},
		{name: "https uppercase scheme", rawURL: "HTTPS://api.example.com", wantErr: false},
		{name: "https with port", rawURL: "https://api.example.com:8443", wantErr: false},
		{name: "http rejected", rawURL: "http://api.example.com", wantErr: true},
		{name: "http localhost allowed", rawURL: "http://localhost:8080", wantErr: false},
		{name: "http 127.0.0.1 allowed", rawURL: "http://127.0.0.1:8080", wantErr: false},
		{name: "http ::1 allowed", rawURL: "http://[::1]:8080", wantErr: false},
		{name: "http on non-loopback hostname resembling localhost rejected", rawURL: "http://localhost.attacker.example", wantErr: true},
		{name: "invalid URL rejected", rawURL: "http://[::1", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := apiRequireHTTPS(tc.rawURL)
			if tc.wantErr && err == nil {
				t.Fatalf("apiRequireHTTPS(%q): got nil error, want error", tc.rawURL)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("apiRequireHTTPS(%q): got error %v, want nil", tc.rawURL, err)
			}
		})
	}
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

// runRealAPICommand parses args against the real `api` command (including
// its command-level parsing options, not just its flags), nested under a
// root command as in cmd/lfx/main.go, and hands the parsed *cli.Command
// to fn in place of runAPI.
func runRealAPICommand(t *testing.T, args []string, fn func(cmd *cli.Command)) {
	t.Helper()
	api := NewAPICommand()
	called := false
	api.Action = func(_ context.Context, cmd *cli.Command) error {
		called = true
		fn(cmd)
		return nil
	}
	root := &cli.Command{Name: "lfx", Commands: []*cli.Command{api}}
	if err := root.Run(context.Background(), append([]string{"lfx", "api"}, args...)); err != nil {
		t.Fatalf("root.Run: %v", err)
	}
	if !called {
		t.Fatal("api action was not invoked")
	}
}

func TestAPIRequestBodyCommaInValueIsOneField(t *testing.T) {
	runRealAPICommand(t, []string{
		"--raw-field", "a=x,b=y",
		"--field", "c=1,active=true",
		"/v1/example",
	}, func(cmd *cli.Command) {
		body, _, err := apiRequestBody(cmd)
		if err != nil {
			t.Fatalf("apiRequestBody: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("json.Unmarshal(body): %v", err)
		}
		want := map[string]any{"a": "x,b=y", "c": "1,active=true"}
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

func TestAPIHeaderCommaInValueIsOneHeader(t *testing.T) {
	runRealAPICommand(t, []string{
		"-H", "X: a,Authorization: z",
		"/v1/example",
	}, func(cmd *cli.Command) {
		headers := cmd.StringSlice(apiHeaderFlagName)
		if len(headers) != 1 || headers[0] != "X: a,Authorization: z" {
			t.Fatalf("headers = %q, want exactly one element %q", headers, "X: a,Authorization: z")
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

func TestAPIRequestBodyRejectsExplicitEmptyInput(t *testing.T) {
	newAPITestCommand(t, []string{"--input="}, func(cmd *cli.Command) {
		if _, _, err := apiRequestBody(cmd); err == nil {
			t.Fatal("apiRequestBody: got nil error, want error for explicit empty --input=")
		}
	})
}

func TestAPIRequestBodyExplicitEmptyInputRejectsCombiningWithFields(t *testing.T) {
	newAPITestCommand(t, []string{"--input=", "--field", "name=example"}, func(cmd *cli.Command) {
		if _, _, err := apiRequestBody(cmd); err == nil {
			t.Fatal("apiRequestBody: got nil error, want error for explicit empty --input= combined with --field")
		}
	})
}

func TestAPIHasExplicitBody(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "no body flags", args: nil, want: false},
		{name: "field", args: []string{"--field", "name=example"}, want: true},
		{name: "raw-field", args: []string{"--raw-field", "name=example"}, want: true},
		{name: "input file", args: []string{"--input", "/dev/null"}, want: true},
		{name: "input stdin marker", args: []string{"--input", "-"}, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			newAPITestCommand(t, tc.args, func(cmd *cli.Command) {
				if got := apiHasExplicitBody(cmd); got != tc.want {
					t.Errorf("apiHasExplicitBody() = %v, want %v", got, tc.want)
				}
			})
		})
	}
}

func TestAPIRequestBodyStatError(t *testing.T) {
	// Substitute an already-closed file for os.Stdin so Stat() fails,
	// simulating the rare platforms/conditions where stdin can't be
	// probed at all; apiRequestBody should surface that error rather
	// than silently falling back to an empty body.
	f, err := os.CreateTemp(t.TempDir(), "closed-stdin")
	if err != nil {
		t.Fatalf("os.CreateTemp: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}

	orig := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = orig })

	newAPITestCommand(t, nil, func(cmd *cli.Command) {
		if _, _, err := apiRequestBody(cmd); err == nil {
			t.Fatal("apiRequestBody: got nil error, want error from failed stdin Stat()")
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

// recordedRequest captures what the test server received for one request.
type recordedRequest struct {
	method string
	uri    string
	header http.Header
	body   string
}

// startAPITestServer logs the test in (plain-file credentials in a temp state
// directory), points the development environment's API audience at a local
// httptest server, and returns the server along with the requests it
// recorded. Loopback http:// URLs are exempt from apiRequireHTTPS, so no TLS
// setup is needed.
func startAPITestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	store, err := credstore.New(credstore.Options{Insecure: true})
	if err != nil {
		t.Fatalf("credstore.New: %v", err)
	}
	if err := store.SaveCredentials(credstore.Credentials{
		RefreshToken:      "test-refresh-token",
		AccessToken:       "test-access-token",
		AccessTokenExpiry: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	if err := store.SaveDeviceState(credstore.DeviceState{
		IDPDomain:   authDomains[envDevelopment],
		Environment: string(envDevelopment),
		Insecure:    true,
	}); err != nil {
		t.Fatalf("SaveDeviceState: %v", err)
	}

	var requests []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, recordedRequest{
			method: r.Method,
			uri:    r.URL.RequestURI(),
			header: r.Header.Clone(),
			body:   string(body),
		})
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	original := defaultAudiences[envDevelopment]
	defaultAudiences[envDevelopment] = server.URL + "/"
	t.Cleanup(func() { defaultAudiences[envDevelopment] = original })

	return server, &requests
}

// runAPICommand runs `lfx --insecure-storage api <args>` through a root
// command carrying the credential flags, as cmd/lfx/main.go wires it, and
// returns what runAPI wrote to stdout along with its error. cli.Exit errors
// are returned rather than terminating the test process.
func runAPICommand(t *testing.T, args ...string) (string, error) {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	originalStdout := os.Stdout
	os.Stdout = writer
	outputCh := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		outputCh <- string(data)
	}()

	root := &cli.Command{
		Name:           "lfx",
		Flags:          CredentialStoreFlags,
		Commands:       []*cli.Command{NewAPICommand()},
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
	}
	runErr := root.Run(context.Background(), append([]string{"lfx", "--insecure-storage", "api"}, args...))

	os.Stdout = originalStdout
	_ = writer.Close()
	return <-outputCh, runErr
}

func TestRunAPIGetSendsBearerTokenAndPrintsBody(t *testing.T) {
	_, requests := startAPITestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	out, err := runAPICommand(t, "/projects?slug=foo&limit=2")
	if err != nil {
		t.Fatalf("runAPI: %v", err)
	}
	if out != `{"ok":true}` {
		t.Errorf("stdout = %q, want the response body verbatim", out)
	}
	if len(*requests) != 1 {
		t.Fatalf("got %d requests, want 1", len(*requests))
	}
	got := (*requests)[0]
	if got.method != http.MethodGet {
		t.Errorf("method = %s, want GET", got.method)
	}
	if got.uri != "/projects?slug=foo&limit=2" {
		t.Errorf("request URI = %q, want the path and query sent verbatim", got.uri)
	}
	if auth := got.header.Get("Authorization"); auth != "Bearer test-access-token" {
		t.Errorf("Authorization = %q, want the stored access token", auth)
	}
	if got.body != "" {
		t.Errorf("body = %q, want none for a plain GET", got.body)
	}
}

func TestRunAPIFieldPromotesGetToPost(t *testing.T) {
	_, requests := startAPITestServer(t, func(w http.ResponseWriter, _ *http.Request) {})

	if _, err := runAPICommand(t, "/projects", "--field", "count=3", "--raw-field", "name=demo"); err != nil {
		t.Fatalf("runAPI: %v", err)
	}
	got := (*requests)[0]
	if got.method != http.MethodPost {
		t.Errorf("method = %s, want POST when a body is supplied without --method", got.method)
	}
	if ct := got.header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got.body), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", got.body, err)
	}
	if body["count"] != float64(3) || body["name"] != "demo" {
		t.Errorf("body = %v, want count=3 (typed) and name=demo (string)", body)
	}
}

func TestRunAPIExplicitMethodWinsOverPromotion(t *testing.T) {
	_, requests := startAPITestServer(t, func(w http.ResponseWriter, _ *http.Request) {})

	if _, err := runAPICommand(t, "-X", "put", "/projects/1", "--raw-field", "name=demo"); err != nil {
		t.Fatalf("runAPI: %v", err)
	}
	if got := (*requests)[0].method; got != http.MethodPut {
		t.Errorf("method = %s, want PUT (case-insensitive --method must not be promoted to POST)", got)
	}
}

func TestRunAPIInputFileDefaultsContentTypeToJSON(t *testing.T) {
	_, requests := startAPITestServer(t, func(w http.ResponseWriter, _ *http.Request) {})
	file := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(file, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := runAPICommand(t, "-X", "POST", "--input", file, "/things"); err != nil {
		t.Fatalf("runAPI: %v", err)
	}
	got := (*requests)[0]
	if got.body != `{"a":1}` {
		t.Errorf("body = %q, want the file contents", got.body)
	}
	if ct := got.header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json for a POST body without one", ct)
	}
}

func TestRunAPIUserHeaderOverridesInjectedOnes(t *testing.T) {
	_, requests := startAPITestServer(t, func(w http.ResponseWriter, _ *http.Request) {})

	if _, err := runAPICommand(t, "-H", "Authorization: Bearer other", "-H", "X-Custom: yes", "/projects"); err != nil {
		t.Fatalf("runAPI: %v", err)
	}
	got := (*requests)[0]
	if auth := got.header.Get("Authorization"); auth != "Bearer other" {
		t.Errorf("Authorization = %q, want the user-supplied header to replace the injected one", auth)
	}
	if got.header.Get("X-Custom") != "yes" {
		t.Errorf("X-Custom = %q, want yes", got.header.Get("X-Custom"))
	}
}

func TestRunAPIQueryFiltersResponseWithoutTrailingNewline(t *testing.T) {
	_, _ = startAPITestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"project":{"slug":"lfx","id":7}}`))
	})

	out, err := runAPICommand(t, "-q", "project.slug", "/projects/7")
	if err != nil {
		t.Fatalf("runAPI: %v", err)
	}
	if out != "lfx" {
		t.Errorf("stdout = %q, want the bare filtered value with no trailing newline", out)
	}
}

func TestRunAPINon2xxPrintsBodyAndExitsWithStatus(t *testing.T) {
	_, _ = startAPITestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"missing"}`))
	})

	out, err := runAPICommand(t, "/nope")
	if out != `{"error":"missing"}` {
		t.Errorf("stdout = %q, want the error body still written", out)
	}
	var exitErr cli.ExitCoder
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %v, want a cli.ExitCoder", err)
	}
	if exitErr.ExitCode() != 1 || !strings.Contains(exitErr.Error(), "HTTP 404") {
		t.Errorf("exit = %d %q, want 1 and a message naming HTTP 404", exitErr.ExitCode(), exitErr.Error())
	}
}

func TestRunAPIDoesNotFollowRedirects(t *testing.T) {
	_, requests := startAPITestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new", http.StatusFound)
		}
	})

	_, err := runAPICommand(t, "/old")
	var exitErr cli.ExitCoder
	if !errors.As(err, &exitErr) || !strings.Contains(exitErr.Error(), "HTTP 302") {
		t.Fatalf("err = %v, want the 302 reported like any other non-2xx status", err)
	}
	if len(*requests) != 1 {
		t.Errorf("got %d requests, want 1 (the redirect target must not be requested with the bearer token)", len(*requests))
	}
}

func TestRunAPIRejectsInvalidInputBeforeSendingARequest(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing path", args: nil, want: "usage: lfx api <path>"},
		{name: "extra path", args: []string{"/a", "/b"}, want: "usage: lfx api <path>"},
		{name: "unsupported method", args: []string{"-X", "PATCH", "/a"}, want: "invalid --method"},
		{name: "malformed header", args: []string{"-H", "NoColon", "/a"}, want: "expected 'key:value'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, requests := startAPITestServer(t, func(w http.ResponseWriter, _ *http.Request) {})

			_, err := runAPICommand(t, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
			if len(*requests) != 0 {
				t.Errorf("got %d requests, want none", len(*requests))
			}
		})
	}
}

func TestRunAPIRequiresLogin(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	_, err := runAPICommand(t, "/projects")
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v, want a not-logged-in error", err)
	}
}
