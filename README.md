# gh-pages

This branch is served by GitHub Pages at
https://linuxfoundation.github.io/lfx-cli/. It is a static asset branch,
not source code, and should generally only be updated by:

- The **Publish Tagged Release** workflow (`.github/workflows/release-tag.yml`
  on `main`), which regenerates `docs/cli.md` from `lfx docs` on every
  tagged release.
- Manual updates to `install.sh` or `client-metadata.json` when the install
  flow or Auth0 CIMD client metadata changes (see LFXV2-2512 / LFXV2-2513).

## Contents

- `install.sh` -- curl-style installer: `curl -sSL https://linuxfoundation.github.io/lfx-cli/install.sh | sh`
- `client-metadata.json` -- CIMD (Client ID Metadata Document) for the LFX
  CLI's Auth0 Device Code client. This URL *is* the client_id; see
  LFXV2-2513 for the corresponding `auth0-terraform` client definition.
- `docs/cli.md` -- generated CLI reference documentation (`lfx docs`),
  republished on each tagged release.
