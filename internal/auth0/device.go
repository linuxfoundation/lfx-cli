// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package auth0 configures the Auth0 Device Authorization Grant (RFC 8628)
// and refresh-token exchange used by `lfx auth login` and `lfx auth token`,
// on top of golang.org/x/oauth2's device flow support
// (Config.DeviceAuth/DeviceAccessToken and TokenSource-based refresh).
package auth0

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
)

// Environment identifies which LFX Auth0 tenant/IdP a login targets.
type Environment string

// Supported environments, selected via the `--env` flag. Each maps to a
// fixed IdP domain and a static, pre-provisioned Auth0 native application
// client ID (device_code + refresh_token grants; see LFXV2-2513 and
// auth0-terraform PR #348). CIMD was evaluated and abandoned for this flow:
// Auth0 silently drops CIMD client registration for the device_code grant.
const (
	EnvProd        Environment = "prod"
	EnvStaging     Environment = "staging"
	EnvDevelopment Environment = "development"
)

// domains maps each Environment to its Auth0 tenant domain.
var domains = map[Environment]string{
	EnvProd:        "linuxfoundation.auth0.com",
	EnvStaging:     "linuxfoundation-staging.auth0.com",
	EnvDevelopment: "linuxfoundation-dev.auth0.com",
}

// clientIDs maps each Environment to its compiled-in, pre-provisioned
// Auth0 native application client ID for the device code grant.
// cspell:disable -- opaque, randomly-generated Auth0 client IDs, not words.
var clientIDs = map[Environment]string{
	EnvProd:        "kkCpM0c9zJ0vNZZDDOGqcyzocOBircOn",
	EnvStaging:     "9XzXgDfAB9O7IoHqhBj5mg4VLvdBM8ci",
	EnvDevelopment: "0TN1OElqQY146vLEPdV5qfejRKpc9IAZ",
}

// cspell:enable

// DefaultAudience is the production LFX v2 API audience used unless
// overridden via `--audience`. It intentionally does not vary with `--env`:
// per LFXV2-2515, the audience is independent of the selected environment
// and must be set explicitly when testing a non-prod API.
const DefaultAudience = "https://lfx-api.v2.cluster.lfx.dev/"

// ErrInvalidEnvironment is returned by Resolve for an unrecognized
// Environment value.
var ErrInvalidEnvironment = errors.New("auth0: invalid environment")

// Resolve returns the IdP domain and client ID for env.
func Resolve(env Environment) (domain, clientID string, err error) {
	domain, ok := domains[env]
	if !ok {
		return "", "", fmt.Errorf("%w: %q (must be one of prod, staging, development)", ErrInvalidEnvironment, env)
	}
	return domain, clientIDs[env], nil
}

// Client drives the Auth0 device code and refresh-token exchanges for a
// single environment, via an underlying oauth2.Config. Auth0's device code
// application is a public native client (Token Endpoint Authentication
// Method: None; is_first_party = true), so ClientSecret is deliberately
// left unset.
type Client struct {
	// Domain is the Auth0 tenant domain, e.g. "linuxfoundation.auth0.com".
	Domain string
	// ClientID is the Auth0 application client ID used for both the
	// device code request and subsequent token/refresh exchanges.
	ClientID string
	// HTTPClient is used for all requests. Defaults to
	// http.DefaultClient if nil.
	HTTPClient *http.Client
}

// config builds the oauth2.Config used for a device code flow requesting
// scopes, pointed at this Client's Auth0 tenant.
func (c *Client) config(scopes []string) *oauth2.Config {
	return &oauth2.Config{
		ClientID: c.ClientID,
		Scopes:   scopes,
		Endpoint: oauth2.Endpoint{
			DeviceAuthURL: "https://" + c.Domain + "/oauth/device/code",
			TokenURL:      "https://" + c.Domain + "/oauth/token",
			AuthStyle:     oauth2.AuthStyleInParams,
		},
	}
}

// context attaches Client's HTTPClient to ctx, per the convention
// documented on oauth2.HTTPClient, so all requests made by the resulting
// oauth2.Config use it.
func (c *Client) context(ctx context.Context) context.Context {
	if c.HTTPClient == nil {
		return ctx
	}
	return context.WithValue(ctx, oauth2.HTTPClient, c.HTTPClient)
}

// DeviceCode wraps the RFC 8628 §3.2 device authorization response,
// carrying the oauth2.Config needed to complete the flow via Poll.
type DeviceCode struct {
	*oauth2.DeviceAuthResponse
	cfg *oauth2.Config
	ctx context.Context //nolint:containedctx // ctx is captured for reuse by Poll, matching Client.RequestDeviceCode/Poll's split across a user-facing pause.
}

// RequestDeviceCode starts the device authorization flow for the given
// audience and scopes.
func (c *Client) RequestDeviceCode(ctx context.Context, audience string, scopes []string) (*DeviceCode, error) {
	cfg := c.config(scopes)
	authCtx := c.context(ctx)

	var opts []oauth2.AuthCodeOption
	if audience != "" {
		opts = append(opts, oauth2.SetAuthURLParam("audience", audience))
	}

	resp, err := cfg.DeviceAuth(authCtx, opts...)
	if err != nil {
		return nil, fmt.Errorf("auth0: request device code: %w", err)
	}
	return &DeviceCode{DeviceAuthResponse: resp, cfg: cfg, ctx: authCtx}, nil
}

// Errors surfaced by Poll when the token endpoint reports the user has
// explicitly declined (RFC 8628 §3.5's "access_denied") or the device code
// expired before the flow completed ("expired_token"). Poll otherwise
// blocks internally on "authorization_pending"/"slow_down" until one of
// these, success, or ctx's deadline (bounded by the device code's own
// expiry).
var (
	// ErrAccessDenied indicates the user explicitly declined the request.
	ErrAccessDenied = errors.New("auth0: access denied")
	// ErrExpiredToken indicates the device code expired before the user
	// completed the flow.
	ErrExpiredToken = errors.New("auth0: device code expired")
	// ErrInvalidGrant indicates the supplied refresh token is expired,
	// revoked, or otherwise no longer valid (Auth0's "invalid_grant"
	// error from the refresh_token grant). Callers should treat this as
	// requiring the user to run `lfx auth login` again.
	ErrInvalidGrant = errors.New("auth0: invalid or expired refresh token")
)

// Poll blocks until the user completes (or rejects) the device flow,
// polling the token endpoint at the interval Auth0 specified (respecting
// "slow_down" backoff) via oauth2.Config.DeviceAccessToken. It returns
// ErrAccessDenied or ErrExpiredToken for those respective outcomes.
func (dc *DeviceCode) Poll() (*oauth2.Token, error) {
	tok, err := dc.cfg.DeviceAccessToken(dc.ctx, dc.DeviceAuthResponse)
	if err == nil {
		return tok, nil
	}

	var retrieveErr *oauth2.RetrieveError
	if errors.As(err, &retrieveErr) {
		switch retrieveErr.ErrorCode {
		case "access_denied":
			return nil, ErrAccessDenied
		case "expired_token":
			return nil, ErrExpiredToken
		}
	}
	return nil, fmt.Errorf("auth0: poll for token: %w", err)
}

// RefreshToken exchanges a refresh token for a new access token.
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (*oauth2.Token, error) {
	cfg := c.config(nil)
	tok, err := cfg.TokenSource(c.context(ctx), &oauth2.Token{RefreshToken: refreshToken}).Token()
	if err == nil {
		return tok, nil
	}

	var retrieveErr *oauth2.RetrieveError
	if errors.As(err, &retrieveErr) && retrieveErr.ErrorCode == "invalid_grant" {
		return nil, ErrInvalidGrant
	}
	return nil, fmt.Errorf("auth0: refresh token: %w", err)
}
