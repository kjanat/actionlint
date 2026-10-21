# Tool configuration ownership

## Status

Accepted.

## Context

Ruff configuration decoding validated a generated rule catalog and Python targets,
encoded native TOML arguments, and supplied a target default before the execution
backend was selected. A valid preview configuration was rejected before the Ruff
WebAssembly engine could validate it. The editor schema repeated that restriction.

The external ShellCheck component also carried an older WIT definition than the
host, despite both advertising the same interface version. Loading the actual
component exposed a failure that the host's example plugins did not exercise.

## Decision

The configuration package validates the host wrapper and preserves engine options.
The Ruff adapter projects flat compatibility settings into the engine's JSON
configuration. The selected engine owns selectors, Python targets, preview
behavior, option validation and defaults. Missing options remain missing.

Native TOML encoding, process restrictions and provisioning policy belong to
`internal/tools/ruff`. The WebAssembly route receives JSON without passing through
those process restrictions. Both routes retain the shared script scheduling,
source mapping and diagnostic pipeline. Native registrations expose a configured
backend loader; `internal/tools/adapters` and `internal/localapp` bind its advertised
routes to the native runner. The application consumes the resulting registrations.
Native adapters do not import the generic script-engine implementation.

The editor schema describes the wrapper without a release-specific Ruff catalog.
Engine metadata supplies engine-specific schemas. Published schema snapshots
remain available for existing consumers.

Component plugins use the canonical `plugins/wit/analyzer.wit` contract. A matching
interface name alone does not establish compatibility when record definitions
have changed. Changes to that contract require coordinated guest builds and
versioning before release. Missing fields must not be silently defaulted.

## Consequences and verification

Engine-specific errors appear when an enabled engine validates its settings,
rather than during generic configuration decoding. Static editor validation cannot
claim which selectors a separately selected engine supports. Maintaining another
Go catalog or constraining all engines to native CLI values was rejected.

Application tests cover configured and injected engines, payload preservation,
defaults and validation failure. Native adapter tests cover option boundaries and
read-only restrictions. Real artifact tests use `ACTIONLINT_TEST_RUFF_WASM` and
`ACTIONLINT_TEST_SHELLCHECK_WASM` to check preview findings, invalid settings,
component loading and plugin-owned options through the CLI.
