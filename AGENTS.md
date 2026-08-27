# AGENTS.md

This file provides essential information for AI agents working on the LFX
CLI codebase. It focuses on development workflows, architecture
understanding, and build processes needed for making code changes.

## Repository Overview

The LFX CLI (`lfx`) is a developer-facing command-line tool for
authenticating with the Linux Foundation's LFX platform and making
authenticated API calls, following the same interaction model as the `gh`
CLI (`lfx auth login` → `lfx auth token`).

### Key Technologies

- **Language**: Go (see `go.mod` for the minimum required version)
- **CLI framework**: [`urfave/cli/v3`](https://github.com/urfave/cli) for
  subcommand routing
- **Docs generation**: [`urfave/cli-docs/v3`](https://github.com/urfave/cli-docs)
  for LLM/agent-friendly Markdown reference docs (`lfx docs`)
- **Release automation**: a single `release-tag.yml` GitHub Actions job
  (running on `macos-latest`) cross-compiles linux/windows binaries and
  natively builds cgo-enabled darwin binaries, then uploads all archives
  to the GitHub Release
- **Output**: user-facing command output uses plain `fmt.Println`/
  `fmt.Fprintln`, not `log/slog`. This is intentional: unlike the
  JSON-structured `slog` logging convention used by LFX's long-running
  services, `lfx` is an interactive CLI with no log aggregator consuming
  its output.

## Architecture Overview

```text
lfx-cli/
├── cmd/
│   └── lfx/                # Main application entry point
├── internal/
│   └── commands/           # CLI subcommand implementations
├── .github/workflows/
│   └── release-tag.yml      # Multi-arch release build/upload
├── go.mod                   # Go module definition
├── Makefile                 # Build automation
├── README.md                 # User documentation
└── AGENTS.md                 # This file (AI agent guidelines)
```

### Current State

The CLI implements `lfx auth login` / `status` / `token` / `logout`
(Auth0 Device Code flow, refresh-token exchange, and credential storage
via the system keychain with `99designs/keyring`, or a plain
`--insecure-storage` fallback) and `lfx api` (raw authenticated calls
against LFX platform APIs, with custom methods/headers, JSON body
construction via `--field`/`--raw-field`, raw bodies via `--input`/stdin,
and response filtering via `--query`).

**No container build**: this project produces binary artifacts only,
distributed via GitHub Releases, the `install.sh` curl-style installer
hosted on `gh-pages`, and `go install`. There is no Dockerfile, Helm
chart, or container image pipeline.

## Development Workflow

### Prerequisites

```bash
# Verify Go is installed at or above the version in go.mod.
go version
```

### Common Development Tasks

#### 1. Build the CLI

```bash
make build
# or directly: go build -ldflags="-s -w" -o bin/lfx ./cmd/lfx
```

#### 2. Run the CLI

```bash
make run ARGS="auth login"
# or directly: ./bin/lfx auth login
```

#### 3. Code Quality Checks

```bash
make fmt               # Format code
make vet               # Run go vet
make lint              # Run golangci-lint (if installed)
make revive            # Run revive (if installed)
make check             # Run all of the above
```

#### 4. Tests

```bash
make test            # Run Go tests
make test-coverage   # Run tests with coverage report
```

#### 5. Clean Build Artifacts

```bash
make clean
```

## Adding New Commands

Commands are implemented in `internal/commands` and registered with the
root `*cli.Command` in `cmd/lfx/main.go`.

### Command Implementation Steps

1. **Create or extend a file** in `internal/commands/` (e.g., `auth.go` for
   an `auth` subcommand group)
2. **Implement a `New<Name>Command()` function** that returns a
   `*cli.Command`, with nested `Commands` for subcommand groups
3. **Register it** in `cmd/lfx/main.go`'s `Commands` slice

### Example Command Implementation

```go
// Package commands implements the lfx CLI subcommands.
package commands

import (
    "context"
    "fmt"

    "github.com/urfave/cli/v3"
)

// NewExampleCommand builds the `lfx example` command.
func NewExampleCommand() *cli.Command {
    return &cli.Command{
        Name:  "example",
        Usage: "Brief description of what the command does",
        Action: func(_ context.Context, cmd *cli.Command) error {
            fmt.Println("example: not yet implemented")
            return nil
        },
    }
}
```

### Package Comments

Every non-test (`*.go`, not `*_test.go`) file in a package must start with
the same `// Package <name> ...` doc comment immediately above the
`package` declaration. Revive's `package-comments` rule itself only
requires one such comment per package, but MegaLinter's `GO_REVIVE` linter
defaults to `GO_REVIVE_CLI_LINT_MODE: list_of_files`, invoking revive with
a flat list of files instead of `./...`. Under that mode revive loses
per-package grouping and flags any non-test file lacking the comment, so
duplicating the identical comment across every non-test file in a package
is a required workaround for how MegaLinter calls revive here, not an
inherent revive requirement. Do not vary the wording between files in the
same package.

## Documentation Generation

The hidden `lfx docs` command generates Markdown reference documentation
for all commands via `cli-docs.ToMarkdown()`, intended for local agent use
and for publishing to the `gh-pages` branch:

```bash
lfx docs                    # Print to stdout
lfx docs --output ./docs     # Write to ./docs/cli.md
```

## gh-pages Branch

The `gh-pages` branch is a static asset branch (not source code) served at
`https://linuxfoundation.github.io/lfx-cli/`. It holds:

- `install.sh` -- the curl-style installer referenced in the README
- `client-metadata.json` -- the CIMD (Client ID Metadata Document) for the
  LFX CLI's Auth0 Device Code client; this URL *is* the client_id (see
  `auth0-terraform`'s `clients_cimd.tf` for the corresponding client
  definition)
- `docs/cli.md` -- generated CLI reference docs, republished on every
  tagged release by the **Publish Tagged Release** workflow's
  `publish-docs` job (see `.github/workflows/release-tag.yml`)

Only edit `install.sh` or `client-metadata.json` directly on `gh-pages`
when the install flow or CIMD metadata changes; `docs/cli.md` is
regenerated automatically and should not be hand-edited.

## Release Process

Releases follow [semantic versioning](https://semver.org/) (`vMAJOR.MINOR.PATCH`).
The current series is v0.x; do not increment the major version unless
explicitly instructed.

### Version bump guidelines

| Change type                                                                          | Version component          |
|--------------------------------------------------------------------------------------|----------------------------|
| Bug fixes, help text/schema wording tweaks, operational changes (CI, release config) | **patch**                  |
| New commands or substantial updates to existing commands                             | **minor**                  |
| Breaking changes or explicit instruction                                             | **major** (only when told) |

### Cutting a release

Do **not** create or push git tags manually. Instead, use the GitHub
Releases UI (or `gh` CLI) to create a release; GitHub will create the tag
automatically, and the **Publish Tagged Release** GitHub Actions workflow
will build and upload multi-arch binaries to it.

```bash
# Determine the next version by inspecting the latest tag.
LATEST=$(git tag --sort=-v:refname | head -1)
echo "Latest tag: $LATEST"
NEXT=v0.1.1  # bump appropriately from the latest tag

gh release create "$NEXT" \
  --generate-notes \
  --latest
```

After creating the release, verify that the **Publish Tagged Release**
workflow triggered by the new tag completes successfully; otherwise
release binaries may be missing even though the GitHub Release exists.

## Contributing Guidelines

1. **Add Commands**: Create new commands in `internal/commands/` following
   the established pattern
2. **Package Comments**: Every new non-test `*.go` file must include the
   same `// Package <name> ...` doc comment as the rest of its package
   (see "Package Comments" above; `*_test.go` files are exempt)
3. **Dependencies**: Run `go get -u ./... && go mod tidy` before every PR to
   keep dependencies current. This upgrades module dependencies only, not the
   Go toolchain itself (`go.mod`'s `go` directive) -- see the toolchain policy
   below before touching that.
4. **Go toolchain version**: Freely bump `go.mod`'s `go` directive to the
   latest available *patch* release (e.g. `1.X.Y` → `1.X.{Y+1}`) to pick up
   security fixes. Do **not** bump the *minor* version (e.g. `1.X.x` →
   `1.{X+1}.x`) unless the user explicitly asks for it, **and** you've
   validated it against the Go version MegaLinter itself bundles --
   MegaLinter runs several linters (e.g. `golangci-lint`) against its own
   bundled Go version, and a `go.mod` directive newer than that bundled
   version breaks those checks.

   To find MegaLinter's bundled Go version:

   ```bash
   # 1. Find the MegaLinter flavor and pinned version tag used in CI.
   grep -A1 'oxsecurity/megalinter' .github/workflows/*.yml
   # e.g. "uses: oxsecurity/megalinter/flavors/<flavor>@<sha>  # <tag>"

   # 2. Fetch that flavor's Dockerfile and read its GO_ALPINE_VERSION (or
   #    GO_IMAGE_VERSION) build arg.
   curl -s "https://raw.githubusercontent.com/oxsecurity/megalinter/<tag>/flavors/<flavor>/Dockerfile" \
     | grep -i 'GO_ALPINE_VERSION\|GO_IMAGE_VERSION'
   ```

   `go.mod`'s `go` directive must never exceed that bundled version. Staying
   one minor version behind it (rather than matching its minor *and* patch
   exactly) leaves room to always take the latest patch release for security
   fixes without ever being blocked by MegaLinter's own bundled patch version
   lagging behind a newly disclosed vulnerability.

   There's no built-in `go` subcommand to look up the latest patch release
   for a given minor version -- query the official `go.dev/dl` JSON feed
   instead:

   ```bash
   # Find the latest patch release for the minor version pinned in go.mod.
   MINOR=$(grep '^go ' go.mod | awk '{print $2}' | cut -d. -f1,2)
   curl -s "https://go.dev/dl/?mode=json&include=all" \
     | jq -r --arg m "go${MINOR}." '.[].version | select(startswith($m))' \
     | sort -V | tail -1
   ```
5. **Code Quality**: Run `make check` before commits
6. **Documentation**: Update README.md for user-facing changes
