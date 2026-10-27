# 0009 — Extensions: WIT components, capability-negotiated, pull-only

Status: accepted · 2026-10-27

## Context

Prior contracts pushed a fixed request (`jobs`, `expressions`, …) to every
guest. Guests that needed less paid for it. Guests that needed more could
not get it without a host release. Eight WIT files accreted around that
shape.

## Decision

One WIT package `kjanat:actionlint@2.0.0` in `wit/`, versioned with the
host. Shape:

```wit
world extension {
  import host;        // the provider API: fn(query, args) -> result
  export manifest;    // name, version, rules offered, providers required
  export diagnostics; // run(document, occurrence) -> list<diagnostic>
}
```

- `host` exposes providers from `architecture.md` as typed resources
  (`source`, `tree`, `workflow`, `action`, `type-env`, `scripts`, …). A
  guest calls what it needs. The host sends nothing unrequested.
- `manifest.requires` lists the providers a guest will call. The host
  refuses to load a guest that calls an undeclared provider.
- Runtime: `wasmtime` component model, `cranelift`, no WASI network.
  WASI filesystem only for explicitly granted preopens. Per-call fuel and
  memory limits are config.
- Built-in rules use the identical provider trait natively. There is no
  host-only data.

Acceptance test, run in CI: `tests/guests/c-minimal` — a C component that
requires only `source` and `tree`, emits one diagnostic for any document
containing a tab character. It must pass with zero host changes against
every document kind the host can parse.

## Consequences

- Adding a provider never breaks existing guests.
- Removing or changing a provider is a WIT major bump. The manifest's
  declared package version gates load.
- The Rust guest SDK (`extension-sdk`) is thin bindings plus helpers. No
  framework.

## Open

- Formatter/completion/code-action exports: same pattern, later minor
  versions.
- Extension distribution/registry: out of scope for this ADR.
