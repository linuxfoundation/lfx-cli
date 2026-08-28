# gh-pages

This branch is served by GitHub Pages at
https://linuxfoundation.github.io/lfx-cli/. It is a static asset branch,
not source code, and should generally only be updated by:

- The **Publish Tagged Release** workflow (`.github/workflows/release-tag.yml`
  on `main`), which regenerates `docs/cli.md` from `lfx docs` on every
  tagged release.
- Manual updates to `install.sh` or `install.ps1` when the install flow
  changes.

## Contents

- `install.sh` -- curl-style installer for macOS/Linux/Git Bash/WSL:
  `curl -sSL https://linuxfoundation.github.io/lfx-cli/install.sh | sh`
- `install.ps1` -- PowerShell installer for native Windows:
  `irm https://linuxfoundation.github.io/lfx-cli/install.ps1 | iex`
- `docs/cli.md` -- generated CLI reference documentation (`lfx docs`),
  republished on each tagged release.
