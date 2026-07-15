// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package commands implements the lfx CLI subcommands.
package commands

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

// NewAPICommand builds the `lfx api` command for making raw authenticated
// calls against LFX platform APIs.
//
// This is currently a stub; the real implementation lands in LFXV2-2517.
func NewAPICommand() *cli.Command {
	return &cli.Command{
		Name:      "api",
		Usage:     "Make an authenticated call to an LFX platform API endpoint",
		ArgsUsage: "<method> <path>",
		Action: func(_ context.Context, _ *cli.Command) error {
			fmt.Println("lfx api: not yet implemented (see LFXV2-2517)")
			return nil
		},
	}
}
