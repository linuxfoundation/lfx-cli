// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package commands implements the lfx CLI subcommands.
package commands

import (
	"errors"
	"fmt"
	"sort"
)

// authEnvironment identifies which LFX Auth0 tenant/IdP a login targets,
// selected via the `--env` flag.
type authEnvironment string

// Supported environments. Each maps to a fixed IdP domain and a static,
// pre-provisioned Auth0 native application client ID (device_code +
// refresh_token grants). CIMD was evaluated and abandoned for this flow:
// Auth0 silently drops CIMD client registration for the device_code grant.
const (
	envProduction  authEnvironment = "production"
	envStaging     authEnvironment = "staging"
	envDevelopment authEnvironment = "development"
)

// authDomains maps each authEnvironment to the Auth0 IdP domain end users
// authenticate against, matching auth0-terraform's own `auth0_domain`
// variable. This is deliberately not each tenant's *.auth0.com domain: prod
// fronts its tenant with the custom domain sso.linuxfoundation.org, and
// since this CLI never calls the Auth0 Management API (only the device
// code and token endpoints), there's no need to separately track the
// underlying tenant name.
//
// `runAPI`'s --hostname gate (api.go) restricts redirecting the API base
// URL to development-environment logins: the goal is to keep a prod or
// staging token -- the ones with real authority -- from ever being sent
// to a third-party host, since --hostname's troubleshooting use case only
// comes up in development anyway. That restriction is also independently
// safe, since each tenant here is a distinct OAuth2 issuer and resource
// servers validate a token's issuer (`iss`), not just its audience: even
// a development-issued token claiming the prod audience would still be
// rejected by prod as untrusted.
var authDomains = map[authEnvironment]string{
	envProduction:  "sso.linuxfoundation.org",
	envStaging:     "linuxfoundation-staging.auth0.com",
	envDevelopment: "linuxfoundation-dev.auth0.com",
}

// authClientIDs maps each authEnvironment to its compiled-in,
// pre-provisioned Auth0 native application client ID for the device code
// grant.
// cspell:disable -- opaque, randomly-generated Auth0 client IDs, not words.
var authClientIDs = map[authEnvironment]string{
	envProduction:  "kkCpM0c9zJ0vNZZDDOGqcyzocOBircOn",
	envStaging:     "9XzXgDfAB9O7IoHqhBj5mg4VLvdBM8ci",
	envDevelopment: "0TN1OElqQY146vLEPdV5qfejRKpc9IAZ",
}

// cspell:enable

// defaultAudiences maps each authEnvironment to the matching LFX v2 API
// audience used when `lfx auth login` is run without an explicit
// `--audience` override.
var defaultAudiences = map[authEnvironment]string{
	envProduction:  "https://lfx-api.v2.cluster.lfx.dev/",
	envStaging:     "https://lfx-api.staging.v2.cluster.linuxfound.info/",
	envDevelopment: "https://lfx-api.dev.v2.cluster.linuxfound.info/",
}

// errInvalidEnvironment is returned by resolveEnvironment for an
// unrecognized authEnvironment value.
var errInvalidEnvironment = errors.New("invalid environment")

// envAliases maps accepted short-forms to the canonical authEnvironment
// constant. Only aliases are listed here; canonical names are valid
// inputs to resolveEnvironment on their own via the authDomains lookup,
// so they don't need a duplicate entry.
var envAliases = map[string]authEnvironment{
	"prod":    envProduction,
	"stage":   envStaging,
	"stg":     envStaging,
	"develop": envDevelopment,
	"dev":     envDevelopment,
}

// normalizeEnvironment maps an input string (canonical name or alias) to
// the canonical authEnvironment value. Unrecognized inputs are returned
// as-is so that resolveEnvironment can produce a single, consistent error.
//
// This same lookup is also applied to authEnvironment values loaded from
// a previously persisted credstore.DeviceState.Environment (see auth.go's
// loadDeviceStateForBackend, resolveAccessToken, and the `auth status`
// display): "prod" was the canonical production name before it was
// renamed to "production", and envAliases still maps it to envProduction,
// so state.json files written before that rename keep resolving
// correctly without requiring a fresh `lfx auth login`.
func normalizeEnvironment(input string) authEnvironment {
	if canonical, ok := envAliases[input]; ok {
		return canonical
	}
	return authEnvironment(input)
}

// resolveEnvironment returns the IdP domain and client ID for env.
func resolveEnvironment(env authEnvironment) (domain, clientID string, err error) {
	domain, ok := authDomains[env]
	if !ok {
		return "", "", fmt.Errorf(
			"%w: %q; see `lfx auth environments` for accepted values",
			errInvalidEnvironment, env,
		)
	}
	return domain, authClientIDs[env], nil
}

// defaultAudienceForEnvironment returns the default LFX v2 API audience
// for env.
func defaultAudienceForEnvironment(env authEnvironment) (string, error) {
	audience, ok := defaultAudiences[env]
	if !ok {
		return "", fmt.Errorf(
			"%w: %q; see `lfx auth environments` for accepted values",
			errInvalidEnvironment, env,
		)
	}
	return audience, nil
}

// environmentAliases returns the aliases accepted for env's canonical
// name, sorted for stable, readable output (e.g. from `lfx auth
// environments`).
func environmentAliases(env authEnvironment) []string {
	var aliases []string
	for alias, canonical := range envAliases {
		if canonical == env {
			aliases = append(aliases, alias)
		}
	}
	sort.Strings(aliases)
	return aliases
}
