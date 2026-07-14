// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package commands implements the lfx CLI subcommands.
package commands

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

// NewAuthCommand builds the `lfx auth` command group with its subcommands.
//
// All subcommands are currently stubs; real implementations land in
// LFXV2-2513 (Auth0 client), LFXV2-2514 (keychain storage), LFXV2-2515
// (login flow), and LFXV2-2516 (token command).
func NewAuthCommand() *cli.Command {
	return &cli.Command{
		Name:  "auth",
		Usage: "Manage authentication with the LFX platform",
		Commands: []*cli.Command{
			newAuthLoginCommand(),
			newAuthTokenCommand(),
			newAuthStatusCommand(),
			newAuthLogoutCommand(),
		},
	}
}

func newAuthLoginCommand() *cli.Command {
	return &cli.Command{
		Name:  "login",
		Usage: "Log in to the LFX platform via the Auth0 Device Code flow",
		Action: func(_ context.Context, _ *cli.Command) error {
			fmt.Println("lfx auth login: not yet implemented (see LFXV2-2515)")
			return nil
		},
	}
}

func newAuthTokenCommand() *cli.Command {
	return &cli.Command{
		Name:  "token",
		Usage: "Print a valid access token for the LFX platform",
		Action: func(_ context.Context, _ *cli.Command) error {
			fmt.Println("lfx auth token: not yet implemented (see LFXV2-2516)")
			return nil
		},
	}
}

func newAuthStatusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Show the current authentication status",
		Action: func(_ context.Context, _ *cli.Command) error {
			fmt.Println("lfx auth status: not yet implemented (see LFXV2-2515)")
			return nil
		},
	}
}

func newAuthLogoutCommand() *cli.Command {
	return &cli.Command{
		Name:  "logout",
		Usage: "Remove stored LFX platform credentials",
		Action: func(_ context.Context, _ *cli.Command) error {
			fmt.Println("lfx auth logout: not yet implemented (see LFXV2-2515)")
			return nil
		},
	}
}
