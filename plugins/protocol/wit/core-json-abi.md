# Core JSON transport

This transport is independent of plugin package identity. Its memory ABI is
version 1; operation payloads are negotiated separately. Component consumers use
the adjacent WIT interfaces instead.

## Memory ABI 1

Exports: `memory`, `abi_version() -> i32`, `alloc(i32) -> i32`,
`dealloc(i32)`, `metadata() -> i64`, `check(i32,i32) -> i64`, and
`validate_config(i32,i32) -> i64`. `abi_version` returns 1. Integers are unsigned
bit patterns. Requests and responses are UTF-8 JSON.

The host allocates and writes each request. Zero from `alloc` refuses allocation.
A response packs its pointer into
the high 32 bits and byte length into the low 32 bits. The guest owns a distinct
response allocation; the host copies its bytes and calls `dealloc` for both
allocations. The guest must not invalidate a buffer before `dealloc`. No import or filesystem
access is implied. A trap invalidates that invocation. The host bounds requests,
responses, memory and execution time independently of advertised limits.

## Discovery 1

`metadata` returns an object with `metadata_version: 1`, `abi_version: 1`,
`transport: "wasm-core-json"`, plugin `id`, `name`, `plugin_version`,
`engine: {name,version}`, `rules`, `config_schema`, `capabilities`, and
`interfaces`. Each operation maps to an ordered array of `{version,export}`.
Exports must have the ABI signatures above. Identifiers select declared
contracts; they never select package-specific behavior.

## Check 1

Request: `{source,filename,config}`. Source is a sanitized snippet; filename is
virtual; config is an engine-owned JSON object. Response:
`{ok,diagnostics,errors}`. Diagnostics use script-engine WIT field semantics,
with JSON `end_location` for `end-location`, Unicode scalar coordinates,
exclusive ends, string severity, and optional name/url. The host validates diagnostics
even when `ok` is false. No template effects may be inferred from
message text or rule codes.

## Check 2

Same request and response envelope as check 1. Every diagnostic additionally
requires `template_effect`, a kebab-case case of the enum in
[`template-effects.wit`](template-effects.wit). Discovery must declare
`template_contract: "kjanat:actionlint/template-effects@0.1.0"`. A host supporting
this contract selects check 2 and rejects missing or unknown effects. Check 1
consumers do not apply provenance filtering.
Template-aware engines must declare `capabilities.script_shells` or
`capabilities.script_inputs` routes. Engines without route metadata use the
explicitly selected native adapter's routing defaults.

## Check 3

Check 3 adds what script-engine 0.3.0 adds to the component interface: fixes,
findings in files the engine read, excluded codes and the request directory. A
host supporting it selects check 3 whenever discovery advertises it and checks
1 or 2 otherwise. Without `template_contract`, check 3 extends check 1; with it,
it extends check 2 and every diagnostic requires `template_effect`.

Discovery declares the capabilities in `capabilities`: `fixes` (boolean),
`reported_sources` (boolean) and `resources`, a list whose only allowed entry is
`"directory"`. They take effect only with check 3.

Request: `{source,filename,config,excluded,directory}`. `excluded` is always
present: a list of diagnostic codes, such as `"SC1091"`, the host's preparation
excludes; the engine must not report them. `directory` is present only when the
engine requests the `directory` resource and the script's working directory is
known, exists and lies inside the workspace; a working directory outside the
workspace fails the check. It is the host path of that directory and grants no access, because core
modules import nothing; it anchors reported sources.

Each diagnostic may add `fixes` and `source`:

- `fixes` is a list of `{description,replacements}`; each replacement is
  `{row,end_row,column,end_column,text}` in snippet coordinates with an
  exclusive end. A fix has at least one replacement. Fixes require
  `capabilities.fixes`.
- `source` names a file the engine read besides the snippet, as a relative
  slash-separated path below the request directory, with no `.` or `..`
  elements, backslashes or colons. The diagnostic's positions are then that
  file's, and it has no fixes. Sources require `capabilities.reported_sources`
  and a request that carried `directory`.

The host drops a diagnostic that uses an undeclared capability and reports an
error. Check 1 and check 2 define neither member; a host ignores them there.

## Validate-config 1

Request is the engine-owned configuration JSON object. Response uses
`{ok,errors}`; diagnostics are not needed. A missing advertised operation means
the engine does not provide configuration validation.
