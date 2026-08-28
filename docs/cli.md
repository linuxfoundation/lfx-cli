# NAME

lfx - Authenticate with and call the LFX platform APIs

# SYNOPSIS

lfx

```
[--backend]=[value]
[--help|-h]
[--insecure-storage]
[--version|-v]
```

**Usage**:

```
lfx [GLOBAL OPTIONS] [command [COMMAND OPTIONS]] [ARGUMENTS...]
```

# GLOBAL OPTIONS

**--backend**="": Pin credential storage to a specific system backend (see `lfx auth backends`); mutually exclusive with --insecure-storage

**--help, -h**: show help

**--insecure-storage**: Store & retrieve credentials in a plain (unencrypted) file instead of the system backend

**--version, -v**: print the version


# COMMANDS

## auth

Manage authentication with the LFX platform

**--help, -h**: show help

### login

Log in to the LFX platform via the Auth0 Device Code flow

**--audience**="": Auth0 API audience to request tokens for (independent of --env) (default: "https://lfx-api.v2.cluster.lfx.dev/")

**--env**="": Target environment: prod, staging, or development (default: "prod")

**--help, -h**: show help

**--web, -w**: Automatically open the verification URL in the default browser

**--with-token**: Read a refresh token from stdin instead of performing the interactive Device Code flow

#### help, h

Shows a list of commands or help for one command

### token

Print a valid access token for the LFX platform

**--help, -h**: show help

#### help, h

Shows a list of commands or help for one command

### status

Show the current authentication status

**--help, -h**: show help

#### help, h

Shows a list of commands or help for one command

### logout

Remove stored LFX platform credentials

**--help, -h**: show help

#### help, h

Shows a list of commands or help for one command

### backends

List the system credential-store backends available on this OS

**--help, -h**: show help

#### help, h

Shows a list of commands or help for one command

### help, h

Shows a list of commands or help for one command

## api

Make an authenticated call to an LFX platform API endpoint

**--field, -F**="": Add a typed JSON body field as 'key=value' (repeatable)

**--header, -H**="": Add an additional request header as 'key:value' (repeatable)

**--help, -h**: show help

**--hostname**="": Override the LFX API base URL (advanced; independent of the IdP domain). Requires a development-environment login (`lfx auth login --env=development`).

**--input**="": Read the request body from a file, or '-' for stdin (Content-Type defaults to application/json for POST/PUT unless overridden with -H)

**--method, -X**="": HTTP method: GET, POST, PUT, or DELETE (default: GET, or POST if a body is explicitly supplied) (default: "GET")

**--query, -q**="": Filter the response body through a gjson expression before output

**--raw-field**="": Add a string JSON body field as 'key=value' (repeatable)

### help, h

Shows a list of commands or help for one command

## help, h

Shows a list of commands or help for one command
