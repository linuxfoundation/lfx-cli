#!/bin/sh
# Copyright The Linux Foundation and each contributor to LFX.
# SPDX-License-Identifier: MIT
#
# Installs the lfx CLI by downloading the latest (or a pinned) release
# binary from GitHub Releases, verifying its checksum, and placing it on
# the PATH.
#
# Usage:
#   curl -sSL https://linuxfoundation.github.io/lfx-cli/install.sh | sh
#
# Environment variables:
#   LFX_CLI_VERSION     Version tag to install (e.g. "v0.1.0"). Defaults to
#                        the latest release.
#   LFX_CLI_INSTALL_DIR Directory to install the "lfx" binary into. Defaults
#                        to /usr/local/bin if writable, else ~/.local/bin.

set -eu

REPO="linuxfoundation/lfx-cli"
BINARY_NAME="lfx"

log() {
	printf '%s\n' "$*" >&2
}

die() {
	log "error: $*"
	exit 1
}

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || die "required command '$1' not found"
}

need_cmd curl
need_cmd tar
need_cmd mktemp

# Detect OS.
detect_os() {
	os=$(uname -s)
	case "$os" in
	Linux) echo "linux" ;;
	Darwin) echo "darwin" ;;
	MINGW* | MSYS* | CYGWIN*) echo "windows" ;;
	*) die "unsupported operating system: $os" ;;
	esac
}

# Detect architecture, normalizing to GoReleaser's goarch names.
detect_arch() {
	arch=$(uname -m)
	case "$arch" in
	x86_64 | amd64) echo "amd64" ;;
	arm64 | aarch64) echo "arm64" ;;
	*) die "unsupported architecture: $arch" ;;
	esac
}

os=$(detect_os)
arch=$(detect_arch)

if [ "$os" = "windows" ] && [ "$arch" = "arm64" ]; then
	die "windows/arm64 is not a published build target"
fi

# Resolve the version to install. Query the GitHub API rather than following
# the "/releases/latest" redirect so we get the tag name directly.
version="${LFX_CLI_VERSION:-}"
if [ -z "$version" ]; then
	log "Determining latest release..."
	version=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" |
		grep '"tag_name":' | head -n1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
	[ -n "$version" ] || die "could not determine latest release version"
fi

log "Installing lfx ${version} for ${os}/${arch}..."

if [ "$os" = "windows" ]; then
	archive_ext="zip"
else
	archive_ext="tar.gz"
fi

archive_name="lfx-cli_${os}_${arch}.${archive_ext}"
base_url="https://github.com/${REPO}/releases/download/${version}"
archive_url="${base_url}/${archive_name}"
checksums_url="${base_url}/checksums.txt"

work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT INT TERM

log "Downloading ${archive_url}..."
curl -fsSL -o "${work_dir}/${archive_name}" "$archive_url" ||
	die "failed to download ${archive_url}; check that ${version} was published for ${os}/${arch}"

log "Downloading checksums..."
curl -fsSL -o "${work_dir}/checksums.txt" "$checksums_url" ||
	die "failed to download checksums.txt from ${base_url}"

# Verify the archive checksum.
verify_checksum() {
	expected=$(grep "  ${archive_name}\$" "${work_dir}/checksums.txt" | awk '{print $1}')
	[ -n "$expected" ] || die "no checksum entry found for ${archive_name}"

	if command -v sha256sum >/dev/null 2>&1; then
		actual=$(sha256sum "${work_dir}/${archive_name}" | awk '{print $1}')
	elif command -v shasum >/dev/null 2>&1; then
		actual=$(shasum -a 256 "${work_dir}/${archive_name}" | awk '{print $1}')
	else
		die "neither sha256sum nor shasum is available to verify the download"
	fi

	[ "$expected" = "$actual" ] ||
		die "checksum mismatch for ${archive_name}: expected ${expected}, got ${actual}"
}

log "Verifying checksum..."
verify_checksum

log "Extracting..."
if [ "$archive_ext" = "zip" ]; then
	need_cmd unzip
	unzip -q -o "${work_dir}/${archive_name}" -d "$work_dir"
else
	tar -xzf "${work_dir}/${archive_name}" -C "$work_dir"
fi

binary_src="${work_dir}/${BINARY_NAME}"
if [ "$os" = "windows" ]; then
	binary_src="${binary_src}.exe"
fi
[ -f "$binary_src" ] || die "extracted archive did not contain ${binary_src}"
chmod +x "$binary_src"

# Pick an install directory: honor LFX_CLI_INSTALL_DIR, else prefer
# /usr/local/bin if writable, else fall back to ~/.local/bin.
install_dir="${LFX_CLI_INSTALL_DIR:-}"
if [ -z "$install_dir" ]; then
	if [ -w "/usr/local/bin" ] 2>/dev/null; then
		install_dir="/usr/local/bin"
	else
		install_dir="${HOME}/.local/bin"
	fi
fi

mkdir -p "$install_dir" || die "could not create install directory ${install_dir}"
[ -w "$install_dir" ] || die "install directory ${install_dir} is not writable (try: sudo LFX_CLI_INSTALL_DIR=/usr/local/bin sh -c '...' or set LFX_CLI_INSTALL_DIR)"

dest="${install_dir}/${BINARY_NAME}"
if [ "$os" = "windows" ]; then
	dest="${dest}.exe"
fi

cp "$binary_src" "$dest"
chmod +x "$dest"

log "Installed lfx ${version} to ${dest}"

case ":${PATH}:" in
*":${install_dir}:"*) ;;
*) log "note: ${install_dir} is not in your PATH; add it, e.g.: export PATH=\"\$PATH:${install_dir}\"" ;;
esac

log "Run '${BINARY_NAME} --help' to get started."
