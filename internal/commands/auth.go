// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package commands implements the lfx CLI subcommands.
package commands

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/linuxfoundation/lfx-cli/internal/auth0"
	"github.com/linuxfoundation/lfx-cli/internal/credstore"
	"github.com/urfave/cli/v3"
)

// insecureStorageFlagName is the auth command group's flag controlling
// whether credentials bypass the system keychain in favor of a plain,
// unencrypted file.
const insecureStorageFlagName = "insecure-storage"

// Flag names shared by the login command.
const (
	webFlagName       = "web"
	withTokenFlagName = "with-token"
	envFlagName       = "env"
	audienceFlagName  = "audience"
)

// scopes requested during the device code flow. offline_access is required
// to receive a refresh token; the rest identify the user for `auth status`.
var loginScopes = []string{"openid", "profile", "email", "offline_access"}

// NewAuthCommand builds the `lfx auth` command group with its subcommands.
//
// The --insecure-storage flag is shared by all subcommands (it is not
// declared as a "Local" flag, so urfave/cli resolves it for subcommand
// actions via cmd.Bool) and controls whether credentials bypass the system
// keychain in favor of credstore's plain (unencrypted) file fallback, e.g.
// for headless/CI use.
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

// credStoreFromCommand builds a credstore.Store using the group-level
// --insecure-storage flag, however deep in the `auth` subcommand tree cmd
// is.
func credStoreFromCommand(cmd *cli.Command) (credstore.Store, error) {
	return credstore.New(credstore.Options{Insecure: cmd.Bool(insecureStorageFlagName)})
}

func newAuthLoginCommand() *cli.Command {
	return &cli.Command{
		Name:  "login",
		Usage: "Log in to the LFX platform via the Auth0 Device Code flow",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    webFlagName,
				Aliases: []string{"w"},
				Usage:   "Automatically open the verification URL in the default browser",
			},
			&cli.BoolFlag{
				Name:  withTokenFlagName,
				Usage: "Read a refresh token from stdin instead of performing the interactive Device Code flow",
			},
			&cli.StringFlag{
				Name:  envFlagName,
				Usage: "Target environment: prod, staging, or development",
				Value: string(auth0.EnvProd),
			},
			&cli.StringFlag{
				Name:  audienceFlagName,
				Usage: "Auth0 API audience to request tokens for (independent of --env)",
				Value: auth0.DefaultAudience,
			},
		},
		Action: runAuthLogin,
	}
}

func runAuthLogin(ctx context.Context, cmd *cli.Command) error {
	store, err := credStoreFromCommand(cmd)
	if err != nil {
		return err
	}

	env := auth0.Environment(cmd.String(envFlagName))
	domain, clientID, err := auth0.Resolve(env)
	if err != nil {
		return err
	}
	audience := cmd.String(audienceFlagName)

	if cmd.Bool(withTokenFlagName) {
		return loginWithToken(store, env, domain, audience)
	}

	client := &auth0.Client{Domain: domain, ClientID: clientID}
	return loginWithDeviceCode(ctx, cmd, client, store, env, domain, audience)
}

// loginWithToken implements `--with-token`: it reads a refresh token from
// stdin (one line, trimmed) for headless/CI use, e.g.
// `echo "$REFRESH_TOKEN" | lfx auth login --with-token`. No access token is
// cached; the next `lfx auth token` call exchanges the refresh token for
// one.
func loginWithToken(store credstore.Store, env auth0.Environment, domain, audience string) error {
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read refresh token from stdin: %w", err)
	}
	refreshToken := strings.TrimSpace(line)
	if refreshToken == "" {
		return errors.New("no refresh token provided on stdin")
	}

	if err := store.SaveCredentials(credstore.Credentials{RefreshToken: refreshToken}); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	if err := store.SaveDeviceState(credstore.DeviceState{
		IDPDomain:   domain,
		Environment: string(env),
		Audience:    audience,
	}); err != nil {
		return fmt.Errorf("save device state: %w", err)
	}

	fmt.Println("Logged in with a supplied refresh token.")
	return nil
}

// loginWithDeviceCode performs the interactive Auth0 Device Code flow:
// request a device code, show the user code and verification URL
// (optionally opening it in a browser), then poll until the user completes
// or the flow expires/is denied.
func loginWithDeviceCode(
	ctx context.Context,
	cmd *cli.Command,
	client *auth0.Client,
	store credstore.Store,
	env auth0.Environment,
	domain, audience string,
) error {
	dc, err := client.RequestDeviceCode(ctx, audience, loginScopes)
	if err != nil {
		return err
	}

	fmt.Printf("First copy your one-time code: %s\n", dc.UserCode)
	if cmd.Bool(webFlagName) {
		fmt.Printf("Opening %s in your browser...\n", dc.VerificationURIComplete)
		if err := openBrowser(dc.VerificationURIComplete); err != nil {
			fmt.Printf("Couldn't open browser automatically: %v\n", err)
			fmt.Printf("Please visit: %s\n", dc.VerificationURIComplete)
		}
	} else {
		fmt.Printf("Then visit: %s\n", dc.VerificationURIComplete)
	}
	fmt.Println("Waiting for authentication...")

	// Poll blocks internally until success or failure, honoring the
	// device code's own expiry and Auth0's requested polling interval
	// (including "slow_down" backoff).
	token, err := dc.Poll()
	switch {
	case err == nil:
	case errors.Is(err, auth0.ErrAccessDenied):
		return errors.New("login was denied")
	case errors.Is(err, auth0.ErrExpiredToken):
		return errors.New("device code expired before login completed")
	default:
		return err
	}

	if err := store.SaveCredentials(credstore.Credentials{
		RefreshToken:      token.RefreshToken,
		AccessToken:       token.AccessToken,
		AccessTokenExpiry: token.Expiry,
	}); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	if err := store.SaveDeviceState(credstore.DeviceState{
		IDPDomain:   domain,
		Environment: string(env),
		Audience:    audience,
	}); err != nil {
		return fmt.Errorf("save device state: %w", err)
	}

	fmt.Println("Login successful.")
	if idToken, ok := token.Extra("id_token").(string); ok {
		if identity := identityFromIDToken(idToken); identity != "" {
			fmt.Printf("Logged in as %s.\n", identity)
		}
	}
	return nil
}

func newAuthTokenCommand() *cli.Command {
	return &cli.Command{
		Name:  "token",
		Usage: "Print a valid access token for the LFX platform",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			store, err := credStoreFromCommand(cmd)
			if err != nil {
				return err
			}

			creds, err := store.LoadCredentials()
			if errors.Is(err, credstore.ErrNotFound) {
				return errors.New("not logged in; run `lfx auth login` first")
			}
			if err != nil {
				return err
			}

			if creds.ValidAccessToken() {
				fmt.Println(creds.AccessToken)
				return nil
			}

			if creds.RefreshToken == "" {
				return errors.New("no refresh token available; run `lfx auth login` again")
			}

			state, err := store.LoadDeviceState()
			if err != nil {
				return fmt.Errorf("load device state: %w", err)
			}

			_, clientID, err := auth0.Resolve(auth0.Environment(state.Environment))
			if err != nil {
				return err
			}
			client := &auth0.Client{Domain: state.IDPDomain, ClientID: clientID}

			token, err := client.RefreshToken(ctx, creds.RefreshToken)
			if errors.Is(err, auth0.ErrInvalidGrant) {
				return errors.New("session expired or revoked; run `lfx auth login` to log in again")
			}
			if err != nil {
				return fmt.Errorf("refresh access token: %w", err)
			}

			refreshToken := token.RefreshToken
			if refreshToken == "" {
				// Auth0 may not rotate the refresh token on every
				// exchange; keep the existing one in that case.
				refreshToken = creds.RefreshToken
			}
			if err := store.SaveCredentials(credstore.Credentials{
				RefreshToken:      refreshToken,
				AccessToken:       token.AccessToken,
				AccessTokenExpiry: token.Expiry,
			}); err != nil {
				return fmt.Errorf("save refreshed credentials: %w", err)
			}

			fmt.Println(token.AccessToken)
			return nil
		},
	}
}

func newAuthStatusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Show the current authentication status",
		Action: func(_ context.Context, cmd *cli.Command) error {
			store, err := credStoreFromCommand(cmd)
			if err != nil {
				return err
			}

			creds, err := store.LoadCredentials()
			if errors.Is(err, credstore.ErrNotFound) {
				fmt.Println("Not logged in.")
				return nil
			}
			if err != nil {
				return err
			}

			state, err := store.LoadDeviceState()
			if err != nil && !errors.Is(err, credstore.ErrNotFound) {
				return fmt.Errorf("load device state: %w", err)
			}

			backend := "system keychain"
			if cmd.Bool(insecureStorageFlagName) {
				backend = "plain file (--insecure-storage)"
			}

			fmt.Println("Logged in.")
			if state.Environment != "" {
				fmt.Printf("  Environment: %s\n", state.Environment)
			}
			if state.IDPDomain != "" {
				fmt.Printf("  IdP domain:  %s\n", state.IDPDomain)
			}
			if state.Audience != "" {
				fmt.Printf("  Audience:    %s\n", state.Audience)
			}
			fmt.Printf("  Credential backend: %s\n", backend)
			if creds.ValidAccessToken() {
				fmt.Printf("  Access token expires: %s\n", creds.AccessTokenExpiry.Format(time.RFC3339))
			} else {
				fmt.Println("  Access token: expired or not cached (will refresh on next `lfx auth token`)")
			}

			return nil
		},
	}
}

func newAuthLogoutCommand() *cli.Command {
	return &cli.Command{
		Name:  "logout",
		Usage: "Remove stored LFX platform credentials",
		Action: func(_ context.Context, cmd *cli.Command) error {
			store, err := credStoreFromCommand(cmd)
			if err != nil {
				return err
			}

			if err := store.DeleteCredentials(); err != nil {
				return fmt.Errorf("delete credentials: %w", err)
			}
			if err := store.DeleteDeviceState(); err != nil {
				return fmt.Errorf("delete device state: %w", err)
			}

			fmt.Println("Logged out.")
			return nil
		},
	}
}

// identityFromIDToken extracts a human-readable identifier (email or
// subject) from an unverified decode of the ID token's JWT payload. It is
// used only for a friendly "Logged in as ..." message; the access token
// (verified server-side on every API call) is the actual credential.
func identityFromIDToken(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return ""
	}

	payload, err := base64URLDecode(parts[1])
	if err != nil {
		return ""
	}

	var claims struct {
		Email string `json:"email"`
		Sub   string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}

	if claims.Email != "" {
		return claims.Email
	}
	return claims.Sub
}

// base64URLDecode decodes a base64url-encoded JWT segment, tolerating the
// missing padding that JWTs conventionally omit.
func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// openBrowser opens url in the user's default browser, following the
// per-OS conventions used by tools like `gh`.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
