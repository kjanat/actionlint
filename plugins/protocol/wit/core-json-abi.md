# Core JSON transport

Component consumers use the adjacent WIT interfaces.

## Memory ABI 1

The guest exports `memory`, `abi_version() -> i32`, `alloc(i32) -> i32`,
`dealloc(i32)`, `metadata() -> i64`, `check(i32,i32) -> i64`, and
`validate_config(i32,i32) -> i64`. `abi_version` returns 1. Integers are unsigned. Payloads are UTF-8 JSON.

The host allocates and writes each request. `alloc` returns zero to refuse. A response sits in its own allocation and returns its pointer in the high 32 bits and its length in the low 32 bits. The host copies it and calls `dealloc` on both allocations. Buffers must stay valid until `dealloc`. The guest gets no imports or filesystem access. A trap invalidates the invocation. The host enforces its own size, memory and time limits.

## Discovery 1

`metadata` returns an object with `metadata_version: 1`, `abi_version: 1`,
`transport: "wasm-core-json"`, plugin `id`, `name`, `plugin_version`,
`engine: {name,version}`, `rules`, `config_schema`, `capabilities`, and
`interfaces`. Each operation maps to an ordered array of `{version,export}`. Exports must have the signatures above.

## Check 1

The request `{source,filename,config}` carries a sanitized snippet, a virtual filename and the engine's configuration object. The response is `{ok,diagnostics,errors}`. Diagnostics use script-engine WIT semantics with `end_location`, Unicode scalar coordinates, exclusive ends, string severity and optional name and url. The host validates diagnostics even when `ok` is false.

## Check 2

Every check 2 diagnostic requires `template_effect`, a kebab-case case of the [`template-effects.wit`](template-effects.wit) enum. Discovery must declare `template_contract: "kjanat:actionlint/template-effects@0.1.0"`. A supporting host selects check 2 and rejects missing or unknown effects. Check 1 hosts skip provenance filtering.

Template-aware engines must declare `capabilities.script_shells` or `capabilities.script_inputs` routes.

## Check 3

Check 3 adds fixes, findings in other files, excluded codes and the request directory. It extends check 2 with `template_contract` and check 1 without it. A supporting host selects check 3 whenever discovery advertises it.

Discovery `capabilities` declares the booleans `fixes` and `reported_sources` and the list `resources`. The only allowed `resources` entry is `"directory"`. These capabilities apply only to check 3.

The request adds `excluded` and `directory`. `excluded` is always present and lists the codes, such as `"SC1091"`, that the engine must not report. `directory` is the script's working directory as a host path and grants no access. The host sends it only when the engine requests the `directory` resource and the directory is known, exists and lies in the workspace. A directory outside the workspace fails the check.

A diagnostic may add these members:

- `fixes` lists `{description,replacements}` and requires `capabilities.fixes`. Each fix has at least one `{row,end_row,column,end_column,text}` replacement in snippet coordinates with an exclusive end.
- `source` is the relative, slash-separated path of another file below the request directory, without `.` or `..` elements, backslashes or colons. Positions then refer to that file, and the diagnostic may carry no fixes. `source` requires `capabilities.reported_sources` and a request with `directory`.

The host drops a diagnostic that uses an undeclared capability and reports an error. Check 1 and check 2 hosts ignore both members.

## Validate-config 1

The request is the engine's configuration object. The response is `{ok,errors}`. Engines without this operation skip configuration validation.
