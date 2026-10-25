# Core JSON transport

This transport is independent of plugin package identity. Its memory ABI is
version 1; operation payloads are negotiated separately. Component consumers use
the adjacent WIT interfaces instead.

## Memory ABI 1

Exports: `memory`, `abi_version() -> i32`, `alloc(i32) -> i32`,
`dealloc(i32)`, `metadata() -> i64`, `check(i32,i32) -> i64`, and
`validate_config(i32,i32) -> i64`. `abi_version` returns 1. Integers are unsigned
bit patterns. Requests and responses are UTF-8 JSON.

The host allocates and writes each request, retaining ownership until the call
returns. Zero from `alloc` refuses allocation. A response packs its pointer into
the high 32 bits and byte length into the low 32 bits. The guest owns a distinct
response allocation; the host copies its bytes and calls `dealloc` for both
allocations. Buffers must remain valid until deallocated. No import or filesystem
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
exclusive ends, string severity, and optional name/url. Findings remain subject
to validation even when `ok` is false. No template effects may be inferred from
message text or rule codes.

## Check 2

Same request and response envelope as check 1. Every diagnostic additionally
requires `template_effect`, a kebab-case case of the enum in
[`template-effects.wit`](template-effects.wit). Discovery must declare
`template_contract: "kjanat:actionlint/template-effects@0.1.0"`. A host supporting
this contract selects check 2 and rejects missing or unknown effects. Check 1
consumers do not apply provenance filtering. Memory ownership remains ABI 1.
Template-aware engines must declare `capabilities.script_shells` or
`capabilities.script_inputs` routes. Legacy engines without route metadata retain
the explicitly selected native adapter's routing defaults.

## Validate-config 1

Request is the engine-owned configuration JSON object. Response uses
`{ok,errors}`; diagnostics are not needed. A missing advertised operation means
the engine does not provide configuration validation.
