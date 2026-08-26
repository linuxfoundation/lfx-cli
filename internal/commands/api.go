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
	"os"
	"strconv"
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
				Usage:   "HTTP method: GET, POST, PUT, or DELETE",
				Value:   http.MethodGet,
			},
			&cli.StringSliceFlag{
				Name:    apiHeaderFlagName,
				Aliases: []string{"H"},
				Usage:   "Add an additional request header as 'key:value' (repeatable)",
			},
			&cli.StringFlag{
				Name:  apiInputFlagName,
				Usage: "Read the request body from a file, or '-' for stdin",
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
				Usage: "Override the LFX API base URL (advanced; independent of the IdP domain)",
			},
		},
		Action: runAPI,
	}
}

func runAPI(ctx context.Context, cmd *cli.Command) error {
	path := cmd.Args().First()
	if path == "" {
		return errors.New("usage: lfx api <path>")
	}

	method := strings.ToUpper(cmd.String(apiMethodFlagName))
	if !apiAllowedMethods[method] {
		return fmt.Errorf("invalid --%s %q (must be one of GET, POST, PUT, DELETE)", apiMethodFlagName, method)
	}

	token, audience, err := resolveAccessToken(ctx, cmd)
	if err != nil {
		return err
	}

	baseURL := cmd.String(apiHostnameFlagName)
	if baseURL == "" {
		baseURL = audience
	}
	if baseURL == "" {
		return errors.New("no API base URL available; log in with `lfx auth login` or pass --hostname")
	}

	body, contentType, err := apiRequestBody(cmd)
	if err != nil {
		return err
	}

	url, err := apiJoinURL(baseURL, path)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
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
		req.Header.Set(strings.TrimSpace(key), strings.TrimSpace(value))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	output := respBody
	if query := cmd.String(apiQueryFlagName); query != "" {
		output = []byte(gjson.GetBytes(respBody, query).String())
	}
	fmt.Println(string(output))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Fprintf(os.Stderr, "HTTP %d\n", resp.StatusCode)
		return cli.Exit("", 1)
	}

	return nil
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
	if err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, "", fmt.Errorf("read stdin: %w", err)
		}
		return data, "", nil
	}

	return nil, "", nil
}

// coerceFieldValue applies gh-style type coercion to a --field value:
// "true"/"false" become booleans, "null" becomes nil, and numeric strings
// become JSON numbers. Everything else stays a string.
func coerceFieldValue(value string) any {
	switch value {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if n, err := strconv.ParseFloat(value, 64); err == nil {
		return n
	}
	return value
}

// apiJoinURL joins base and path into a single URL, ensuring exactly one
// slash separates them.
func apiJoinURL(base, path string) (string, error) {
	if base == "" {
		return "", errors.New("empty base URL")
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/"), nil
}
