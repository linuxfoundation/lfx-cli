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

**--backend**="": Pin credential storage to a specific system backend (see 'lfx auth backends'); mutually exclusive with --insecure-storage

**--help, -h**: show help

**--insecure-storage**: Store & retrieve credentials in a plain (unencrypted) file instead of the system backend

**--version, -v**: print the version


# COMMANDS

## auth

Manage authentication with the LFX platform

**--help, -h**: show help

### login

Log in to the LFX platform via the Auth0 Device Code flow

**--env**="": Target environment; see 'lfx auth environments' for accepted values (default: "production")

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

### environments

List --env values accepted by `lfx auth login`, including aliases and default audiences

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

**--input**="": Read the request body from a file, or '-' for stdin (Content-Type defaults to application/json for POST/PUT unless overridden with -H)

**--method, -X**="": HTTP method: GET, POST, PUT, or DELETE (default: GET, or POST if a body is explicitly supplied) (default: "GET")

**--query, -q**="": Filter the response body through a gjson expression before output

**--raw-field**="": Add a string JSON body field as 'key=value' (repeatable)

### help, h

Shows a list of commands or help for one command

## query-svc

Query LFX v2 resources and organizations via the query service

    lfx query-svc <command> [options]
    
    Examples:
      # Find a project by its slug, printing its entity ID (project:<project_uid>).
      lfx query-svc query-resources --type project --filters-all slug:tlf -q 'resources.0.id'
    
      # Number of child projects for a given project.
      lfx query-svc query-resources-count --type project --parent <entity-id>
    
      # Find committees with "board" in the name for a given project.
      lfx query-svc query-resources --type committee --parent <entity-id> --name board

**--help, -h**: show help

### query-resources

Locate resources by their type or parent, or search by display name or alias

**--cel-filter**="": CEL expression over data, resource_type, and id

**--date-field**="": Data field to filter by date (requires --date-from or --date-to)

**--date-from**="": Inclusive start, ISO 8601 date or datetime

**--date-to**="": Inclusive end, ISO 8601 date or datetime

**--filter-grants**="": Only resources with direct FGA grants ('direct'); requires --type

**--filters**="": Data field filter as 'field:value' (repeatable)

**--filters-all**="": Data field filter as 'field:value', all must match (repeatable)

**--filters-or**="": Data field filter as 'field:value', at least one must match (repeatable)

**--help, -h**: show help

**--name**="": Resource name or alias; supports typeahead

**--page-size**="": Number of results per page (1-1000) (default: "50")

**--page-token**="": Opaque token for the next page

**--parent**="": Parent reference, e.g. project:<uid>

**--query, -q**="": Filter the response body through a gjson expression before output

**--sort**="": Sort order: name_asc, name_desc, updated_asc, updated_desc, best_match (default: "name_asc")

**--tags**="": Tag to match with OR logic (repeatable)

**--tags-all**="": Tag to match with AND logic (repeatable)

**--type**="": Resource type to search

**--version**="": Version of the API (default: "1")

#### help, h

Shows a list of commands or help for one command

### query-resources-count

Count matching resources, optionally grouped by tag or with a cardinality metric

**--date-field**="": Data field to filter by date (requires --date-from or --date-to)

**--date-from**="": Inclusive start, ISO 8601 date or datetime

**--date-to**="": Inclusive end, ISO 8601 date or datetime

**--filters**="": Data field filter as 'field:value' (repeatable)

**--filters-all**="": Data field filter as 'field:value', all must match (repeatable)

**--filters-or**="": Data field filter as 'field:value', at least one must match (repeatable)

**--group-by**="": Tag prefix to group the count by

**--group-by-size**="": Maximum number of groups (1-1000); requires --group-by

**--help, -h**: show help

**--metric**="": Metric to compute, e.g. cardinality:<tag_prefix>

**--name**="": Resource name or alias; supports typeahead

**--parent**="": Parent reference, e.g. project:<uid>

**--query, -q**="": Filter the response body through a gjson expression before output

**--tags**="": Tag to match with OR logic (repeatable)

**--tags-all**="": Tag to match with AND logic (repeatable)

**--type**="": Resource type to count

**--version**="": Version of the API (default: "1")

#### help, h

Shows a list of commands or help for one command

### query-membership-summary

Summarize membership records per organization and project

**--b2b-org-uid**="": B2B organization UID to summarize

**--help, -h**: show help

**--page-token**="": Opaque token for the next page

**--project-uid**="": Project UID to summarize

**--query, -q**="": Filter the response body through a gjson expression before output

**--version**="": Version of the API (default: "1")

#### help, h

Shows a list of commands or help for one command

### query-orgs

Locate a single organization by name or domain

**--domain**="": Organization domain name, e.g. example.com (no scheme or path)

**--help, -h**: show help

**--name**="": Organization name

**--query, -q**="": Filter the response body through a gjson expression before output

**--version**="": Version of the API (default: "1")

#### help, h

Shows a list of commands or help for one command

### suggest-orgs

Get organization suggestions for a typeahead query

**--help, -h**: show help

**--query, -q**="": Filter the response body through a gjson expression before output

**--search**="": Search query (required)

**--version**="": Version of the API (default: "1")

#### help, h

Shows a list of commands or help for one command

### help, h

Shows a list of commands or help for one command

## help, h

Shows a list of commands or help for one command
