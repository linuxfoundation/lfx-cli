---
name: upgrade-maintenance
description: Upgrade lfx-cli's Go dependencies, including the Goa-generated LFX v2 service modules behind its service commands (e.g. `lfx query-svc`), and reconcile each command's builder-argument table with upstream signature changes. Use when upgrading dependencies, after an upstream LFX v2 service release, or when TestGoaBuilderSignatures fails.
license: MIT
---

# Upgrade Maintenance

lfx-cli exposes LFX v2 services as commands by importing each service's
Goa-generated Go module and calling the generated `Build<Method>Payload`
functions in `gen/http/<svc>/client/cli.go`. They take one `string` per
parameter, in order, and apply Goa's validation and type parsing. A change
in parameter count breaks the build or the test, but **two parameters
swapping places still compiles**, sending a flag's value to the wrong field.

lfx-cli therefore never calls a builder positionally. Each endpoint has an
ordered `[]goaBuilderArg` table (e.g. `queryServiceEndpoints` in
`internal/commands/query.go`). `callGoaBuilder` passes values in table order,
and `TestGoaBuilderSignatures` (`internal/commands/goa_test.go`) parses the
generated source and checks every table's `Param` names against the builder's
parameter names, **in order**. Goa names parameters after their field (e.g.
`querySvcQueryResourcesTags`), so the test catches added, removed, renamed
and reordered parameters.

## Phase 1: Upgrade dependencies

### Step 1.1: Go toolchain

Follow the `lfx-megalinter` skill's Go version policy (see `AGENTS.md`). Do
**not** set `go.mod`'s `go` directive to your local toolchain version.

### Step 1.2: Modules

```bash
git diff --quiet go.mod || echo "go.mod already has uncommitted changes"
go get -u ./...
go mod tidy
git diff go.mod | grep 'linuxfoundation/lfx-v2-'
```

Record each `lfx-v2-*` OLD → NEW version for Phase 2. If a bump already
landed in an earlier commit, diff from before that commit instead (`git log
--oneline -S'lfx-v2-SERVICE vX.Y.Z' -- go.mod`). No working-tree diff only
means nothing changed in *this* pass.

Also note any `goa.design/goa/v3` bump. See Step 2.5.

### Step 1.3: Build and test

```bash
make build
make test
```

`make test` is required: a builder signature change may compile and only
fail `TestGoaBuilderSignatures`.

## Phase 2: Reconcile Goa-backed commands

### Step 2.1: Build failures

- **Undefined `Build<Method>Payload` or client method** (e.g. in a method
  expression like `(*client.Client).QueryOrgs`): the upstream endpoint was
  renamed or removed. Confirm in the module's `gen/<svc>/service.go`
  (`MethodNames`), then rename or delete the spec.
- **`NewClient` signature changed** in `gen/http/<svc>/client/client.go`:
  update the service's `goaService.NewClient`. lfx-cli calls the HTTP-level
  `NewClient` (scheme, host, doer, encoder, decoder, restore), so it is
  **not** affected when the service-level `NewClient` gains a positional
  endpoint argument.

### Step 2.2: `TestGoaBuilderSignatures` failures

The failure prints `got` (the table) and `want` (the generated parameters).
**Never reorder or rename table entries just to make the lists match.** Read
the generated builder body for every differing parameter first:

```bash
MOD=$(go list -m -f '{{.Dir}}' github.com/linuxfoundation/lfx-v2-SERVICE)
sed -n '/^func BuildFooPayload/,/^}/p' "$MOD"/gen/http/*/client/cli.go
```

| Difference | Action |
|---|---|
| Parameter added | Add a `goaBuilderArg` at the same position. Choose `Kind` per Step 2.3, a `Flag` matching Goa's own flag name (see `gen/http/cli/*/cli.go`) unless that name is in `goaReservedFlagNames` (e.g. `--query`, reserved for output filtering; the test fails on a collision), `Usage` from the field comment in `gen/<svc>/service.go`, and `Default` only if Goa's CLI flag has a non-`REQUIRED`, non-empty default. Flag names that differ from Goa's need a human decision. |
| Parameter removed | Delete the entry and its flag. Removing a flag breaks scripts that use it, so mention it in the release notes. Also update any `goaService.Examples` that use the flag. |
| Same names, new order | Reorder the table to match. The flags themselves don't change. |
| Renamed | Check whether the field kept its meaning (`service.go`, the service's changelog or PR). If it did, update `Param` and keep the flag name so existing scripts keep working. If not, treat it as a removal plus an addition. |

### Step 2.3: Choosing a `goaArgKind`

Choose from how the builder parses the parameter, not from its name:

| Builder body | Kind |
|---|---|
| Assigned directly, or parsed with `strconv` (ints, bools) | `goaArgString` (Goa does the parsing and range checks) |
| `json.Unmarshal` into `[]string` | `goaArgJSONList` |
| The `...BearerToken` parameter | `goaArgBearerToken` |
| `json.Unmarshal` into an object or request body (`...Body` param) | **Not supported yet.** Stop and raise it; write commands need a separate design (see "Write commands" below). |

Builders treat `""` as unset for optional parameters, so lfx cannot send an
explicit empty string in a path, query or header parameter. Request bodies,
which are decoded from JSON, are not affected.

### Step 2.4: New endpoints

`TestGoaBuilderSignatures` only covers endpoints lfx already exposes. Compare
the service's methods with its specs:

```bash
go doc github.com/linuxfoundation/lfx-v2-SERVICE/gen/SVC_PKG MethodNames
grep -o 'Name: *"[^"]*"' internal/commands/SERVICE.go
```

Decide for each new endpoint whether to expose it. Health checks
(`readyz`, `livez`) are deliberately left out. A newly exposed endpoint
needs a spec, which `TestGoaBuilderSignatures` then covers automatically.

### Step 2.5: Goa version bumps

If an upstream module was regenerated with a different Goa version, check
that the generator's conventions still hold:

- `Build<Method>Payload` still exists, takes only `string` parameters and
  returns `(payload, error)`. `callGoaBuilder` checks this at runtime and
  reports a clear error, but run each command's `--help` and a real call to
  be sure.
- Typed errors still expose `GoaErrorName()` and a `Message` field
  (`formatGoaError`).
- A change to how Goa names parameters would make every table fail at once.
  Update the `Param` values mechanically only after confirming that the
  order and meaning of the parameters didn't change.

### Step 2.6: Response changes

Commands print the service's raw JSON response (`rawBodyDoer`), not
re-encoded structs, so new response fields appear automatically and nothing
needs updating.

## Write commands (fetch-then-PUT)

No write commands exist yet. When they are added, any command that GETs a
resource, merges flag values into it and PUTs it back to a full-replace
endpoint must decode that GET strictly, rejecting unknown fields. Otherwise
an outdated lfx silently erases fields it doesn't know about. Endpoints
where the server merges partial updates (e.g. member-service key contacts)
don't need this. When they exist, this skill must cover reconciling them
too.

## Phase 3: Verify and summarize

```bash
make check
make test
./bin/lfx query-svc query-resources --type project --page-size 1   # needs `lfx auth login`
```

Summarize for the PR description:

1. The `lfx-v2-*` and Goa version bumps.
2. Table changes: added, removed or renamed flags, and endpoints exposed or
   left out.
3. Anything needing a human decision (unsupported argument kinds, changes in
   a field's meaning).
4. Whether a new lfx release is warranted (user-facing flag changes are at
   least a patch release; see `AGENTS.md`).
