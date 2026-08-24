// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package commands implements the lfx CLI subcommands.
package commands

import (
	"errors"
	"fmt"
)

// authEnvironment identifies which LFX Auth0 tenant/IdP a login targets,
// selected via the `--env` flag.
type authEnvironment string

// Supported environments. Each maps to a fixed IdP domain and a static,
// pre-provisioned Auth0 native application client ID (device_code +
// refresh_token grants). CIMD was evaluated and abandoned for this flow:
// Auth0 silently drops CIMD client registration for the device_code grant.
const (
	envProd        authEnvironment = "prod"
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
var authDomains = map[authEnvironment]string{
	envProd:        "sso.linuxfoundation.org",
	envStaging:     "linuxfoundation-staging.auth0.com",
	envDevelopment: "linuxfoundation-dev.auth0.com",
}

// authClientIDs maps each authEnvironment to its compiled-in,
// pre-provisioned Auth0 native application client ID for the device code
// grant.
// cspell:disable -- opaque, randomly-generated Auth0 client IDs, not words.
var authClientIDs = map[authEnvironment]string{
	envProd:        "kkCpM0c9zJ0vNZZDDOGqcyzocOBircOn",
	envStaging:     "9XzXgDfAB9O7IoHqhBj5mg4VLvdBM8ci",
	envDevelopment: "0TN1OElqQY146vLEPdV5qfejRKpc9IAZ",
}

// cspell:enable

// defaultAudience is the production LFX v2 API audience used unless
// overridden via `--audience`. It intentionally does not vary with `--env`:
// the audience is independent of the selected environment and must be set
// explicitly when testing a non-prod API.
const defaultAudience = "https://lfx-api.v2.cluster.lfx.dev/"

// errInvalidEnvironment is returned by resolveEnvironment for an
// unrecognized authEnvironment value.
var errInvalidEnvironment = errors.New("invalid environment")

// resolveEnvironment returns the IdP domain and client ID for env.
func resolveEnvironment(env authEnvironment) (domain, clientID string, err error) {
	domain, ok := authDomains[env]
	if !ok {
		return "", "", fmt.Errorf("%w: %q (must be one of prod, staging, development)", errInvalidEnvironment, env)
	}
	return domain, authClientIDs[env], nil
}
