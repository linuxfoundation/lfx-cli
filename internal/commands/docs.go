// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package commands implements the lfx CLI subcommands.
package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	clidocs "github.com/urfave/cli-docs/v3"
	"github.com/urfave/cli/v3"
)

// NewDocsCommand builds the hidden `lfx docs` command used to generate
// LLM/agent-friendly Markdown reference documentation for all commands.
func NewDocsCommand() *cli.Command {
	return &cli.Command{
		Name:   "docs",
		Usage:  "Generate Markdown reference documentation for all commands",
		Hidden: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "output",
				Usage: "Directory to write docs/cli.md to; omit to print to stdout",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			md, err := clidocs.ToMarkdown(cmd.Root())
			if err != nil {
				return fmt.Errorf("generating markdown docs: %w", err)
			}

			out := cmd.String("output")
			if out == "" {
				fmt.Println(md)
				return nil
			}

			if err := os.MkdirAll(out, 0o755); err != nil {
				return fmt.Errorf("creating output directory %q: %w", out, err)
			}

			path := filepath.Join(out, "cli.md")
			if err := os.WriteFile(path, []byte(md), 0o644); err != nil {
				return fmt.Errorf("writing %q: %w", path, err)
			}

			fmt.Println("Wrote", path)
			return nil
		},
	}
}
