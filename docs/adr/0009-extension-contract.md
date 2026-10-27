# 0009 — Extensions: WIT components, capability-negotiated, pull-only

Status: accepted · 2026-10-27 · verified against wasmtime 50.0.0, wit-bindgen 0.63.0, component-model spec (main)

## Context

Prior contracts pushed a fixed request (`jobs`, `expressions`, …) to every
guest. Guests that needed less paid for it. Guests that needed more could
not get it without a host release. Eight WIT files accreted around that
shape.

## Decision

One WIT package `kjanat:actionlint@2.0.0` in `wit/`, versioned with the
host. Shape:

```wit
package kjanat:actionlint@2.0.0;

interface source   { resource document { text: func() -> string; /* … */ } }
interface tree     { /* typed CST access, see architecture.md */ }
interface workflow { /* … one interface per provider … */ }

world extension {
  import source;
  import tree;
  import workflow;
  // one import per provider; a guest's world lists only what it uses

  export manifest: func() -> manifest;          // name, version, rules offered
  export diagnostics: interface { run: func(doc: borrow<document>, occurrence: string) -> list<diagnostic>; }
}
```

- **One WIT interface per provider**, typed. Not a generic
  `query(name: string, args: list<u8>)` import. Reason: the component
  model's compatibility story is per-interface and per-version — a typed
  interface gets type validation at load and semver tracking for free. A
  string-keyed RPC gets neither. (WASI's `wasi:config/store` shows a
  generic `get(key) -> option<string>` is legal WIT. It just forfeits the
  type check.) [spec: Explainer.md "Canonical interface name".
  Wasmtime `component::Linker` docs]
- **The guest's `world` import list *is* the capability declaration.**
  The host refuses to link a component whose imports include an interface
  it does not expose. No separate `requires` list in the manifest. That
  would duplicate what the component type already states.
  [component-model.bytecodealliance.org/design/worlds.html]
- The host sends nothing unrequested. Providers are host functions behind
  resources. A guest that never calls `workflow` never triggers workflow
  resolution. This is a consequence of ADR-0004 memoisation, not of the
  WIT.
- Runtime: `wasmtime` with `default-features = false`, features
  `["runtime", "cranelift", "component-model"]` (+`std`, UNVERIFIED whether
  cranelift requires it). `component-model-async` not enabled (docs: "very
  incomplete"). [docs.rs/wasmtime crate features]
- Limits, all exist in 50.0.0:
  - CPU: `Config::consume_fuel(true)` + `Store::set_fuel(u64)` per call,
    or `Config::epoch_interruption(true)` + `Store::set_epoch_deadline`
    with a host ticker. Decision: epoch (cheaper, wall-clock-ish). Fuel
    only for deterministic tests. [docs.rs/wasmtime `Store`, `Config`]
  - Memory: `Store::limiter` with `StoreLimitsBuilder::new()
    .memory_size(n).instances(1).tables(k).build()`.
    [docs.rs/wasmtime `StoreLimitsBuilder`]
  - Stack: `Config::max_wasm_stack` (default 512 KiB).
- WASI: `wasmtime_wasi::WasiCtxBuilder::new()` — no preopens by default,
  network denied by default (`allow_tcp`/`allow_udp`/`allow_ip_name_lookup`
  off. Nothing reachable unless `inherit_network` or `socket_addr_check`
  is called). Host grants read-only preopens only when an extension's
  manifest asks and config allows. [docs.rs/wasmtime-wasi `WasiCtxBuilder`]
- Host bindings: `wasmtime::component::bindgen!` at compile time. Guest
  (Rust): `wit_bindgen::generate!`. Nothing committed (ADR-0012).
- Built-in rules use the identical provider trait natively. There is no
  host-only data.

Acceptance test, run in CI: `tests/guests/c-minimal` — a C component whose
world imports only `source` and `tree`, emits one diagnostic for any
document containing a tab character. Built with `wit-bindgen c` +
wasi-sdk `wasm32-wasip2-clang -mexec-model=reactor`. Verified with
`wasm-tools print | head -1` → `(component`. It must pass with zero host
changes against every document kind the host can parse.
[component-model.bytecodealliance.org/language-support/…/c.html]

## Versioning (from the spec, not assumed)

The runtime matches interface names after canonicalising the version:
major > 0 → keep major only (`2.1.3` → `@2`). `0.x.y` → keep `0.x`.
Prerelease → exact match only. Within a track the higher version wins and
load succeeds if the guest uses a subset of the host's functions.
[Explainer.md "Canonical interface name", wasmtime
`crates/environ/src/component/names.rs`]

Therefore:

- Package starts at **`@2.0.0`**, never `0.x` — `0.x` tracks are per-minor
  and would make every provider addition a break.
- Adding a provider or a function to a provider: minor bump. Existing
  guests load unchanged.
- Removing or changing a signature: major bump. Old guests fail to link
  with a clear error, not at call time.
- A guest built against `@2.3.0` on a `@2.1.0` host links by name and then
  fails type validation if it uses 2.2+ functions. The host reports that as
  "extension requires newer host", from the validation error.

## Consequences

- Each new provider is a new WIT interface + a `bindgen!` regeneration.
  No manifest format to keep in sync.
- The Rust guest SDK (`extension-sdk`) is `wit_bindgen::generate!` plus
  helpers. No framework.
- Non-Rust guests commit nothing here. Their bindings are generated in
  their own build (ADR-0012).

## Open

- `std` feature requirement for the cranelift path: check when the
  workspace is created.
- Formatter/completion/code-action exports: same pattern, later minor
  versions.
- Extension distribution/registry: out of scope for this ADR.
