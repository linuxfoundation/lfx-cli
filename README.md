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
lfx api <path>
lfx api /projects --field name=example   # auto-promotes to POST
lfx api -X PUT /projects/123 --input - -H "If-Match: <ver>" < input.json
```

Credentials (refresh token, cached access token) are stored in your
operating system's credential store by default (macOS Keychain, Windows
Credential Manager, Linux Secret Service/KWallet/`pass`). Which of these is
actually used can vary between invocations on the same machine (e.g. Secret
Service reachable in one shell session but not another); pass
`--backend` to pin it to one explicitly (see `lfx auth backends`
for the available names). Once a login has pinned a backend, later commands
must pass the same `--backend` value. Pass `--insecure-storage` to
any `auth` subcommand to instead store credentials in a plain, unencrypted,
owner-only file, at the cost of weaker protection for the stored tokens. On
Windows, this owner-only mode relies on inherited directory permissions
rather than a real ACL, since Go's `Chmod(0600)` maps to the read-only
attribute there rather than restricting access to the current user.

```bash
lfx auth login --insecure-storage
lfx auth login --backend=keychain
```

Run `lfx --help` or `lfx <command> --help` for full details on any command.

## Development

```bash
make build   # Build ./bin/lfx
make check   # Format, vet, and lint
make test    # Run tests
```

See [AGENTS.md](AGENTS.md) for detailed development workflows and
architecture notes.
