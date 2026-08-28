// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package main provides the lfx CLI binary entry point.
package main

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/linuxfoundation/lfx-cli/internal/commands"
	"github.com/urfave/cli/v3"
)

// version is set at build time via -ldflags by GoReleaser. When unset (e.g.
// `make build` or `go install github.com/linuxfoundation/lfx-cli/cmd/lfx@latest`,
// neither of which supply linker flags), it falls back to the module
// version recorded in the binary's build info, or "dev" if that's
// unavailable.
var version = ""

func init() {
	if version != "" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
		return
	}
	version = "dev"
}

func main() {
	app := &cli.Command{
		Name:                  "lfx",
		Usage:                 "Authenticate with and call the LFX platform APIs",
		Version:               version,
		EnableShellCompletion: true,
		Flags:                 commands.CredentialStoreFlags,
		Commands: []*cli.Command{
			commands.NewAuthCommand(),
			commands.NewAPICommand(),
			commands.NewDocsCommand(),
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
