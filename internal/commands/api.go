// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package commands implements the lfx CLI subcommands.
package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/urfave/cli/v3"
)

// Flag names for the `api` command.
const (
	apiMethodFlagName   = "method"
	apiHeaderFlagName   = "header"
	apiInputFlagName    = "input"
	apiFieldFlagName    = "field"
	apiRawFieldFlagName = "raw-field"
	apiQueryFlagName    = "query"
	apiHostnameFlagName = "hostname"
)

// apiAllowedMethods enumerates the HTTP methods `lfx api` accepts via
// --method. PATCH is deliberately excluded: the LFX API, by design,
// currently includes no PATCH endpoints, favoring PUT with ETag/If-Match
// concurrency control instead.
var apiAllowedMethods = map[string]bool{
	http.MethodGet:    true,
	http.MethodPost:   true,
	http.MethodPut:    true,
	http.MethodDelete: true,
}

// NewAPICommand builds the `lfx api` command for making raw authenticated
// calls against LFX platform APIs.
func NewAPICommand() *cli.Command {
	return &cli.Command{
		Name:      "api",
		Usage:     "Make an authenticated call to an LFX platform API endpoint",
		ArgsUsage: "<path>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    apiMethodFlagName,
				Aliases: []string{"X"},
				Usage:   "HTTP method: GET, POST, PUT, or DELETE (default: GET, or POST if a body is explicitly supplied)",
				Value:   http.MethodGet,
			},
			&cli.StringSliceFlag{
				Name:    apiHeaderFlagName,
				Aliases: []string{"H"},
				Usage:   "Add an additional request header as 'key:value' (repeatable)",
			},
			&cli.StringFlag{
				Name:  apiInputFlagName,
				Usage: "Read the request body from a file, or '-' for stdin (Content-Type defaults to application/json for POST/PUT unless overridden with -H)",
			},
			&cli.StringSliceFlag{
				Name:    apiFieldFlagName,
				Aliases: []string{"F"},
				Usage:   "Add a typed JSON body field as 'key=value' (repeatable)",
			},
			&cli.StringSliceFlag{
				Name:  apiRawFieldFlagName,
				Usage: "Add a string JSON body field as 'key=value' (repeatable)",
			},
			&cli.StringFlag{
				Name:    apiQueryFlagName,
				Aliases: []string{"q"},
				Usage:   "Filter the response body through a gjson expression before output",
			},
			&cli.StringFlag{
				Name:  apiHostnameFlagName,
				Usage: "Override the LFX API base URL (advanced; independent of the IdP domain). Requires a development-environment login (`lfx auth login --env=development`).",
			},
		},
		Action: runAPI,
	}
}

func runAPI(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 1 {
		return errors.New("usage: lfx api <path>")
	}
	path := cmd.Args().First()

	method := strings.ToUpper(cmd.String(apiMethodFlagName))
	if !cmd.IsSet(apiMethodFlagName) && apiHasExplicitBody(cmd) {
		// No --method was passed, but the caller explicitly supplied a
		// body via --field/--raw-field/--input; a GET request wouldn't
		// carry it anywhere useful. Auto-promote to POST, matching the
		// conventional default `gh api` and `curl` both use when a body
		// is present. This never triggers for a body implicitly picked
		// up from piped stdin with no explicit body flag, since that's
		// not a clear enough signal of intent to override the GET
		// default.
		method = http.MethodPost
	}
	if !apiAllowedMethods[method] {
		return fmt.Errorf("invalid --%s %q (must be one of GET, POST, PUT, DELETE)", apiMethodFlagName, method)
	}

	token, audience, env, err := resolveAccessToken(ctx, cmd)
	if err != nil {
		return err
	}

	baseURL := cmd.String(apiHostnameFlagName)
	if cmd.IsSet(apiHostnameFlagName) {
		if baseURL == "" {
			return errors.New("--hostname was set to an empty value; omit the flag to use the login audience instead")
		}
		// --hostname sends the live bearer token to whatever host is
		// named, so it's restricted to development-environment logins
		// (`lfx auth login --env=development`) to limit the blast
		// radius of a leaked or misdirected prod/staging token. This is
		// a heuristic tied to how login is normally done (--env and
		// --audience are set together), not a cryptographic guarantee
		// about what the token itself is scoped to.
		if env != envDevelopment {
			return fmt.Errorf("--%s requires a development-environment login (run `lfx auth login --%s=%s`); the current login's environment is %q", apiHostnameFlagName, envFlagName, envDevelopment, env)
		}
	}
	if baseURL == "" {
		baseURL = audience
	}
	if baseURL == "" {
		return errors.New("no API base URL available; log in with `lfx auth login` or pass --hostname")
	}
	if err := apiRequireHTTPS(baseURL); err != nil {
		return err
	}

	body, contentType, err := apiRequestBody(cmd)
	if err != nil {
		return err
	}

	endpoint, err := apiJoinURL(baseURL, path)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, h := range cmd.StringSlice(apiHeaderFlagName) {
		key, value, ok := strings.Cut(h, ":")
		if !ok {
			return fmt.Errorf("invalid --%s %q (expected 'key:value')", apiHeaderFlagName, h)
		}
		// Header.Set (rather than Add) is deliberate: it lets a
		// repeated -H for the same key (e.g. an explicit
		// "Authorization:" or "Content-Type:") cleanly override the
		// value set above, at the cost of not accumulating multiple
		// values for the same header name the way curl/gh api do.
		req.Header.Set(strings.TrimSpace(key), strings.TrimSpace(value))
	}
	if len(body) > 0 && req.Header.Get("Content-Type") == "" && (method == http.MethodPost || method == http.MethodPut) {
		// LFX platform APIs are overwhelmingly JSON, so default to that
		// for any POST/PUT body unless the caller set Content-Type
		// explicitly (via --field/--raw-field, which already produce
		// JSON and set this above, or via -H, handled just above).
		req.Header.Set("Content-Type", "application/json")
	}

	// Don't auto-follow redirects: http.DefaultClient can preserve the
	// Authorization header across a same-host redirect even when it
	// downgrades from HTTPS to HTTP, which would bypass
	// apiRequireHTTPS and leak the bearer token. A 3xx response is
	// instead reported like any other non-2xx status below.
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if query := cmd.String(apiQueryFlagName); query != "" {
		// --query needs the whole body in memory to run gjson against
		// it, so buffer and filter before writing.
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read response body: %w", err)
		}
		output := []byte(gjson.GetBytes(respBody, query).String())
		// Write output verbatim with no added trailing newline,
		// matching `gh api -q` so a scalar result (e.g. a single
		// field) can be captured cleanly by a shell command
		// substitution without a stray newline.
		if _, err := os.Stdout.Write(output); err != nil {
			return fmt.Errorf("write response body: %w", err)
		}
	} else {
		// Stream the body straight through rather than buffering it
		// all in memory first, since this command explicitly supports
		// redirecting its output for large or binary responses.
		if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
			return fmt.Errorf("write response body: %w", err)
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Use cli.Exit's message (rather than writing directly to
		// stderr) so main's generic `Error: <err>` handling only emits
		// one diagnostic line instead of two.
		return cli.Exit(fmt.Sprintf("HTTP %d", resp.StatusCode), 1)
	}

	return nil
}

// apiHasExplicitBody reports whether cmd was given an explicit
// body-supplying flag (--input, --field, or --raw-field), as opposed to a
// body implicitly picked up from piped stdin with none of those flags set.
// Used to decide whether the default --method should be promoted from GET;
// see runAPI.
func apiHasExplicitBody(cmd *cli.Command) bool {
	return cmd.IsSet(apiInputFlagName) ||
		len(cmd.StringSlice(apiFieldFlagName)) > 0 ||
		len(cmd.StringSlice(apiRawFieldFlagName)) > 0
}

// apiRequestBody constructs the request body and its Content-Type from the
// command's --input, --field, and --raw-field flags (mutually exclusive:
// --input vs. --field/--raw-field), falling back to stdin when none are
// passed and stdin is piped (non-TTY).
func apiRequestBody(cmd *cli.Command) (body []byte, contentType string, err error) {
	input := cmd.String(apiInputFlagName)
	fields := cmd.StringSlice(apiFieldFlagName)
	rawFields := cmd.StringSlice(apiRawFieldFlagName)

	if input != "" && (len(fields) > 0 || len(rawFields) > 0) {
		return nil, "", fmt.Errorf("--%s cannot be combined with --%s or --%s", apiInputFlagName, apiFieldFlagName, apiRawFieldFlagName)
	}

	if input != "" {
		if input == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return nil, "", fmt.Errorf("read stdin: %w", err)
			}
			return data, "", nil
		}
		data, err := os.ReadFile(input)
		if err != nil {
			return nil, "", fmt.Errorf("read --%s file: %w", apiInputFlagName, err)
		}
		return data, "", nil
	}

	if len(fields) > 0 || len(rawFields) > 0 {
		obj := make(map[string]any, len(fields)+len(rawFields))
		for _, f := range fields {
			key, value, ok := strings.Cut(f, "=")
			if !ok {
				return nil, "", fmt.Errorf("invalid --%s %q (expected 'key=value')", apiFieldFlagName, f)
			}
			obj[key] = coerceFieldValue(value)
		}
		for _, f := range rawFields {
			key, value, ok := strings.Cut(f, "=")
			if !ok {
				return nil, "", fmt.Errorf("invalid --%s %q (expected 'key=value')", apiRawFieldFlagName, f)
			}
			obj[key] = value
		}
		data, err := json.Marshal(obj)
		if err != nil {
			return nil, "", fmt.Errorf("build JSON body: %w", err)
		}
		return data, "application/json", nil
	}

	stat, err := os.Stdin.Stat()
	if err != nil {
		return nil, "", fmt.Errorf("stat stdin: %w", err)
	}
	if stat.Mode()&os.ModeCharDevice == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, "", fmt.Errorf("read stdin: %w", err)
		}
		return data, "", nil
	}

	return nil, "", nil
}

// jsonNumberPattern matches valid JSON number syntax (RFC 8259), which is
// stricter than Go's strconv.ParseFloat: it rejects "NaN", "Inf", and
// hexadecimal floats (e.g. "0x1p2"), all of which ParseFloat accepts but
// which are not valid JSON numbers and would otherwise either fail
// json.Marshal outright or silently coerce a value the user likely meant
// as a literal string.
var jsonNumberPattern = regexp.MustCompile(`^-?(0|[1-9]\d*)(\.\d+)?([eE][+-]?\d+)?$`)

// coerceFieldValue applies gh-style type coercion to a --field value:
// "true"/"false" become booleans, "null" becomes nil, and values matching
// JSON number syntax become json.Number (preserving the original digits
// verbatim in the request body, rather than round-tripping through
// float64 and silently losing precision for integers beyond 2^53).
// Everything else stays a string.
func coerceFieldValue(value string) any {
	switch value {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if jsonNumberPattern.MatchString(value) {
		return json.Number(value)
	}
	return value
}

// apiJoinURL joins base and path into a single URL. Unlike a plain string
// concatenation, url.JoinPath percent-encodes path segments and resolves
// traversal components such as "../", which matters since path can come
// from user input and base can come from a user-controlled --hostname.
func apiJoinURL(base, path string) (string, error) {
	if base == "" {
		return "", errors.New("empty base URL")
	}
	return url.JoinPath(base, path)
}

// apiRequireHTTPS rejects base URLs that would send the bearer token over
// a non-HTTPS connection, since the request's Authorization header is
// otherwise transmitted in cleartext. Loopback hosts are exempted, since
// http:// there is a common and low-risk way to point --hostname at a
// local mock server for debugging.
func apiRequireHTTPS(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid base URL %q: %w", rawURL, err)
	}
	if parsed.Scheme == "https" {
		return nil
	}
	switch parsed.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return nil
	}
	return fmt.Errorf("refusing to send credentials to non-HTTPS URL %q (use an https:// --hostname, or localhost/127.0.0.1 for local debugging)", rawURL)
}
