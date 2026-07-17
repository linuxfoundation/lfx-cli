# LFX CLI

`lfx` is a developer-facing command-line tool for authenticating with the
Linux Foundation's LFX platform and making authenticated API calls, following
the same interaction model as the `gh` CLI (`lfx auth login` → `lfx auth
token`).

## Installation

```bash
go install github.com/linuxfoundation/lfx-cli/cmd/lfx@latest
```

Prebuilt binaries for Linux, macOS, and Windows are also published on the
[Releases](https://github.com/linuxfoundation/lfx-cli/releases) page.

## Usage

```bash
# Log in via the Auth0 Device Code flow.
lfx auth login

# Show the current authentication status.
lfx auth status

# Print a valid access token (e.g. for use in scripts or other tools).
lfx auth token

# Log out and remove stored credentials.
lfx auth logout

# Make an authenticated call to an LFX platform API endpoint.
lfx api <method> <path>
```

Credentials (refresh token, cached access token) are stored in your
operating system's credential store by default (macOS Keychain, Windows
Credential Manager, Linux Secret Service/KWallet/`pass`). Pass
`--insecure-storage` to any `auth` subcommand to instead store credentials
in a plain, unencrypted, owner-only file, at the cost of weaker protection
for the stored tokens. On Windows, this owner-only mode relies on inherited
directory permissions rather than a real ACL, since Go's `Chmod(0600)` maps
to the read-only attribute there rather than restricting access to the
current user.

```bash
lfx auth login --insecure-storage
```

Run `lfx --help` or `lfx <command> --help` for full details on any command.

> **Note:** This project is under active development. Authentication and API
> commands are currently stubs; see the
> [LFXV2-2509 epic](https://linuxfoundation.atlassian.net/browse/LFXV2-2509)
> for status.

## Development

```bash
make build   # Build ./bin/lfx
make check   # Format, vet, and lint
make test    # Run tests
```

See [AGENTS.md](AGENTS.md) for detailed development workflows and
architecture notes.
