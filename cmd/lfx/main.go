// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package main provides the lfx CLI binary entry point.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/linuxfoundation/lfx-cli/internal/commands"
	"github.com/urfave/cli/v3"
)

// version is set at build time via -ldflags.
var version = "dev"

func main() {
	app := &cli.Command{
		Name:                  "lfx",
		Usage:                 "Authenticate with and call the LFX platform APIs",
		Version:               version,
		EnableShellCompletion: true,
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
