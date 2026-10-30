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
`LFX_CLI_INSTALL_DIR` to override the install location. This works out of
the box on macOS and Linux, and on Windows under Git Bash or WSL.

On native Windows (PowerShell), use `install.ps1` instead, since Windows has
no `sh` interpreter by default:

```powershell
irm https://linuxfoundation.github.io/lfx-cli/install.ps1 | iex
```

This installs to `%LOCALAPPDATA%\lfx-cli\bin` by default (or
`LFX_CLI_INSTALL_DIR` if set), and honors `LFX_CLI_VERSION` the same way as
`install.sh`.

Alternatively, install with Go:

```bash
go install github.com/linuxfoundation/lfx-cli/cmd/lfx@latest
```

## Usage

Run `lfx --help` or `lfx <command> --help` for full details on any command.

### Authentication

```bash
# Log in via the Auth0 Device Code flow.
lfx auth login

# Automatically open the HTTPS verification URL in your default browser.
lfx auth login --web

# Log in against a non-production environment (aliases accepted). Later
# commands send requests to the API for the environment you logged in to.
lfx auth login --env staging   # also: stage, stg
lfx auth login --env dev       # also: develop, development

# List the available environments and their APIs.
lfx auth environments

# Show the current authentication status.
lfx auth status

# Print a valid access token (e.g. for use in scripts or other tools).
lfx auth token

# Log out and remove stored credentials.
lfx auth logout
```

Credentials (refresh token, cached access token) are stored in your
operating system's credential store by default (macOS Keychain, Windows
Credential Manager, Linux Secret Service/KWallet/`pass`). Which of these is
actually used can vary between invocations on the same machine (e.g. Secret
Service reachable in one shell session but not another); pass
`--backend` to pin it to one explicitly (see `lfx auth backends`
for the available names). Once a login has pinned a backend, later commands
must pass the same `--backend` value. Pass `--insecure-storage` to
instead store credentials in a plain, unencrypted, owner-only file, at the
cost of weaker protection for the stored tokens. On Windows, this
owner-only mode relies on inherited directory permissions rather than a
real ACL, since Go's `Chmod(0600)` maps to the read-only attribute there
rather than restricting access to the current user.

```bash
lfx auth login --insecure-storage
lfx auth login --backend=keychain
```

### Raw API calls

```bash
# Make an authenticated call to an LFX platform API endpoint.
lfx api '/my-grants?v=1&object_type=project'
lfx api /projects --field name=example   # auto-promotes to POST
lfx api -X PUT /projects/123 --input - -H "If-Match: <ver>" < input.json  # Content-Type: application/json is added automatically
```

### Service commands

LFX services have dedicated commands, with a flag for each API parameter
and validation before the request is sent. Like `lfx api`, they use the
logged-in environment, print the JSON response, and accept `--query`/`-q` to
filter it with a gjson expression. Commands for more services are being
added; until a service has its own commands, use `lfx api`. Run
`lfx <service> --help` to list a service's commands.

#### Query service

```bash
# Find a project by its slug, printing its entity ID (project:<project_uid>).
lfx query-svc query-resources --type project --filters-all slug:tlf -q 'resources.0.id'

# Number of child projects for a given project.
lfx query-svc query-resources-count --type project --parent <entity-id>

# Find committees with "board" in the name for a given project.
lfx query-svc query-resources --type committee --parent <entity-id> --name board
```

## Shell completion

The `install.sh` script attempts to set up shell completion for your login
shell. If you installed `lfx` another way, or the installer could not do it,
run `lfx completion --help` for the steps for your shell.

## Development

```bash
make build   # Build ./bin/lfx
make check   # Format, vet, and lint
make test    # Run tests
```

See [AGENTS.md](AGENTS.md) for detailed development workflows and
architecture notes.
