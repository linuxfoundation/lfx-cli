# LFX CLI

`lfx` is a developer-facing command-line tool for authenticating with the
Linux Foundation's LFX platform and making authenticated API calls, following
the same interaction model as the `gh` CLI (`lfx auth login` → `lfx auth
token`).

## Installation

```bash
curl -sSL https://linuxfoundation.github.io/lfx-cli/install.sh | sh
```

This downloads the correct prebuilt binary for your OS/architecture from the
[Releases](https://github.com/linuxfoundation/lfx-cli/releases) page,
verifies its checksum, and installs it to `/usr/local/bin` (or `~/.local/bin`
if that's not writable). Set `LFX_CLI_VERSION` to pin a specific release, or
`LFX_CLI_INSTALL_DIR` to override the install location.

Alternatively, install with Go:

```bash
go install github.com/linuxfoundation/lfx-cli/cmd/lfx@latest
```

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
