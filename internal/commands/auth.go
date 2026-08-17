// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package commands implements the lfx CLI subcommands.
package commands

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

// insecureStorageFlagName is the auth command group's flag controlling
// whether credentials bypass the system keychain in favor of a plain,
// unencrypted file.
const insecureStorageFlagName = "insecure-storage"

// NewAuthCommand builds the `lfx auth` command group with its subcommands.
//
// The --insecure-storage flag is shared by all subcommands and controls
// whether credentials bypass the system keychain in favor of credstore's
// plain (unencrypted) file fallback, e.g. for headless/CI use.
//
// The login, token, status, and logout actions are currently stubs; real
// implementations land in LFXV2-2515 (login flow) and LFXV2-2516 (token
// command), at which point they'll build a credstore.Store via
// credstore.New(credstore.Options{Insecure:
// cmd.Bool(insecureStorageFlagName)}).
func NewAuthCommand() *cli.Command {
	return &cli.Command{
		Name:  "auth",
		Usage: "Manage authentication with the LFX platform",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  insecureStorageFlagName,
				Usage: "Store credentials in a plain (unencrypted) file instead of the system keychain",
			},
		},
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
