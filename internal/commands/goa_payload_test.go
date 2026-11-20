// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package commands

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/urfave/cli/v3"
)

// TestGoaFlagsToBuilderArgs covers the shared conversion from parsed flags
// to builder arguments once per goaArgKind, using a stand-in builder rather
// than a real service. Per-service and per-endpoint behavior comes from the
// generated builders and is guarded by TestGoaBuilderSignatures, so new
// services or endpoints do not need tests like this one; extend it only
// when a new goaArgKind is added.
func TestGoaFlagsToBuilderArgs(t *testing.T) {
	errInvalid := errors.New("invalid")
	builder := func(version, name, list, token string) ([]string, error) {
		if name == "bad" {
			return nil, errInvalid
		}
		return []string{version, name, list, token}, nil
	}
	spec := goaEndpointSpec[struct{}]{
		Name:    "test",
		Builder: builder,
		Args: []goaBuilderArg{
			{Param: "version", Kind: goaArgString, Flag: "version", Default: "1"},
			{Param: "name", Kind: goaArgString, Flag: "name"},
			{Param: "list", Kind: goaArgJSONList, Flag: "list"},
			{Param: "token", Kind: goaArgBearerToken},
		},
	}

	tests := []struct {
		name    string
		args    []string
		want    []string
		wantErr error
	}{
		{
			name: "defaults and omitted list",
			want: []string{"1", "", "", "test-token"},
		},
		{
			name: "repeated list values keep commas",
			args: []string{"--version", "2", "--name", "n", "--list", "a:1", "--list", "b:x,y"},
			want: []string{"2", "n", `["a:1","b:x,y"]`, "test-token"},
		},
		{
			name:    "builder validation error",
			args:    []string{"--name", "bad"},
			wantErr: errInvalid,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payload any
			var buildErr error
			cmd := goaService[struct{}]{}.endpointCommand(spec)
			cmd.Action = func(_ context.Context, cmd *cli.Command) error {
				values, err := goaBuilderValues(cmd, spec.Args, "test-token")
				if err != nil {
					return err
				}
				payload, buildErr = callGoaBuilder(spec.Builder, values)
				return nil
			}
			if err := cmd.Run(context.Background(), append([]string{"test"}, tt.args...)); err != nil {
				t.Fatalf("run: %v", err)
			}
			if !errors.Is(buildErr, tt.wantErr) {
				t.Fatalf("builder error = %v, want %v", buildErr, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			got, _ := payload.([]string)
			if !slices.Equal(got, tt.want) {
				t.Errorf("builder args = %q, want %q", got, tt.want)
			}
		})
	}
}
