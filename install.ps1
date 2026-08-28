# Copyright The Linux Foundation and each contributor to LFX.
# SPDX-License-Identifier: MIT
#
# Installs the lfx CLI by downloading the latest (or a pinned) release
# binary from GitHub Releases, verifying its checksum, and placing it on
# the PATH.
#
# Usage:
#   irm https://linuxfoundation.github.io/lfx-cli/install.ps1 | iex
#
# Environment variables:
#   LFX_CLI_VERSION     Version tag to install (e.g. "v0.1.0"). Defaults to
#                        the latest release.
#   LFX_CLI_INSTALL_DIR Directory to install the "lfx.exe" binary into.
#                        Defaults to "$env:LOCALAPPDATA\lfx-cli\bin".

$ErrorActionPreference = "Stop"

$Repo = "linuxfoundation/lfx-cli"
$BinaryName = "lfx"

function Write-InstallLog($Message) {
    [Console]::Error.WriteLine($Message)
}

function Die($Message) {
    Write-Error "error: $Message"
    exit 1
}

# Detect architecture, normalizing to GoReleaser's goarch names.
function Get-Arch {
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) {
        $arch = $env:PROCESSOR_ARCHITEW6432
    }
    switch ($arch) {
        "AMD64" { return "amd64" }
        "ARM64" { return "arm64" }
        default { Die "unsupported architecture: $arch" }
    }
}

$os = "windows"
$arch = Get-Arch

if ($arch -eq "arm64") {
    Die "windows/arm64 is not a published build target"
}

# Resolve the version to install. Query the GitHub API rather than following
# the "/releases/latest" redirect so we get the tag name directly.
$version = $env:LFX_CLI_VERSION
if ([string]::IsNullOrEmpty($version)) {
    Write-InstallLog "Determining latest release..."
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
        $version = $release.tag_name
    } catch {
        Die "could not determine latest release version: $_"
    }
    if ([string]::IsNullOrEmpty($version)) {
        Die "could not determine latest release version"
    }
}

Write-InstallLog "Installing lfx $version for $os/$arch..."

$archiveName = "lfx-cli_${os}_${arch}.zip"
$baseUrl = "https://github.com/$Repo/releases/download/$version"
$archiveUrl = "$baseUrl/$archiveName"
$checksumsUrl = "$baseUrl/checksums.txt"

$workDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $workDir | Out-Null

try {
    $archivePath = Join-Path $workDir $archiveName
    Write-InstallLog "Downloading $archiveUrl..."
    try {
        Invoke-WebRequest -Uri $archiveUrl -OutFile $archivePath
    } catch {
        Die "failed to download $archiveUrl; check that $version was published for $os/$arch"
    }

    $checksumsPath = Join-Path $workDir "checksums.txt"
    Write-InstallLog "Downloading checksums..."
    try {
        Invoke-WebRequest -Uri $checksumsUrl -OutFile $checksumsPath
    } catch {
        Die "failed to download checksums.txt from $baseUrl"
    }

    Write-InstallLog "Verifying checksum..."
    $checksumLine = Select-String -Path $checksumsPath -Pattern "  $([regex]::Escape($archiveName))$" |
        Select-Object -First 1
    if (-not $checksumLine) {
        Die "no checksum entry found for $archiveName"
    }
    $expected = ($checksumLine.Line -split '\s+')[0]
    $actual = (Get-FileHash -Path $archivePath -Algorithm SHA256).Hash.ToLower()
    if ($expected -ne $actual) {
        Die "checksum mismatch for ${archiveName}: expected $expected, got $actual"
    }

    Write-InstallLog "Extracting..."
    Expand-Archive -Path $archivePath -DestinationPath $workDir -Force

    $binarySrc = Join-Path $workDir "$BinaryName.exe"
    if (-not (Test-Path $binarySrc)) {
        Die "extracted archive did not contain $binarySrc"
    }

    # Pick an install directory: honor LFX_CLI_INSTALL_DIR, else default to
    # "%LOCALAPPDATA%\lfx-cli\bin".
    $installDir = $env:LFX_CLI_INSTALL_DIR
    if ([string]::IsNullOrEmpty($installDir)) {
        $installDir = Join-Path $env:LOCALAPPDATA "lfx-cli\bin"
    }

    New-Item -ItemType Directory -Path $installDir -Force | Out-Null

    $dest = Join-Path $installDir "$BinaryName.exe"
    Copy-Item -Path $binarySrc -Destination $dest -Force

    Write-InstallLog "Installed lfx $version to $dest"

    $pathEntries = $env:PATH -split ";"
    if ($pathEntries -notcontains $installDir) {
        Write-InstallLog "note: $installDir is not in your PATH; add it, e.g.:"
        Write-InstallLog "  [Environment]::SetEnvironmentVariable('Path', `"`$env:Path;$installDir`", 'User')"
    }

    Write-InstallLog "Run '$BinaryName --help' to get started."
} finally {
    Remove-Item -Path $workDir -Recurse -Force -ErrorAction SilentlyContinue
}
