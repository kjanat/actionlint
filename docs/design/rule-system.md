# Unified rule system

Status: shared contracts and host workflows implemented; legacy semantic migration in progress.

## Required outcomes

- [x] Stable document/node references across mappings, scripts, invocations and findings.
- [x] Shared rule catalogue with independent category, subcategory, maturity and provider metadata.
- [x] Structured diagnostic explanations, help, related locations and documentation.
- [x] Safe fix preview/application with overlap and stale-source checks and post-fix analysis.
- [x] LSP document synchronization, diagnostics and code actions.
- [x] Owned YAML tree boundary; no parser-library nodes in semantic engine code.
- [x] Shared native/component rule contracts and working Ruff WebAssembly plugin.
- [x] Generated rule reference and CLI explanations from the catalogue.
- [x] Domain-owned generators, release tooling, conformance and probes.
- [x] Opt-in remote resolution contract and commitish integration.
- [x] Review 5539656825: dynamic matrix insertions and action-metadata duplicate keys.

- [ ] Complete the migration of visitor-based semantic checks.

## Decisions

Public analyzers consume explicit
facts and return findings; the host owns scheduling, configuration selection,
resource lifetime, suppression and severity. Native built-ins and component plugins
share rule metadata and result semantics. Rule identity, provider identity and a
provider's execution backend are separate concepts.

A source reference identifies a document, a content revision, a node and its
structural address. Offsets are locations, not identity. Explicit job/step IDs are
preferred. Anonymous nodes are matched by structural content and occurrence; named-node identities are stable across editor revisions. Anonymous content edits
produce new identities; clients can reconcile them by structural address. Indistinguishable duplicate
nodes require an ordering tie-breaker.

The internal YAML package owns the tree consumed by workflow, mapping and engine semantics. Parser-specific types
are confined to decoding/encoding.

Fixes declare applicability and carry complete edit groups. Application checks
source revisions, ranges and overlaps before writing. Preview and LSP code actions
consume the same edit representation. Analysis runs again after a CLI application. All files are validated before writing, but atomic replacement is per file; an I/O failure can leave a partially applied multi-file change and reports the written files.

Remote resolution is an injected capability with explicit requests, immutable
resolved identities, cancellation and provenance. commitish supplies GitHub ref
semantics; the engine must not recreate those semantics or obtain credentials
implicitly. Hosts must opt into network access.

Generated reference material belongs to the rule catalogue. Generator logic lives
with its owning subsystem.

## Script-tool boundary

`internal/checking/scripts/contract` defines registration, prepared script batches and findings.
`internal/plugins/execution/scripts` adapts the shared snippet protocol to native processes or
an injected WASM backend. Bundled manifests and their jq transforms under
`internal/plugins/manifests/bundled` define each tool's command and output conversion.
`internal/checking/scripts/registry` constructs registrations from those manifests.
`internal/plugins/execution/scripts` selects the explicit or repository-configured transport.
Tool configuration is a map keyed by provider rule name; each bundled provider manifest under
`internal/plugins/manifests/bundled` supplies its configuration schema and shorthands, and
`internal/input/configuration/tools` holds invocation settings. Decoding configuration never imports tool execution.
The engine and application depend on registrations, not these implementations.

`internal/integrations/process` owns process capacity and completion. CLI and LSP startup
compose artifact loading and registrations; independent core engines advertise
shell selectors in metadata. Adding a provider requires no engine-name switch.
Dependency tests enforce both directions, and integration tests exercise an
independent process provider and a remote core-WASM artifact.

## Validation

Verify contracts with native and WASM consumers, final artifacts, source-coordinate
and identity regressions, fix conflicts/staleness, LSP protocol exchanges, remote
provider failures, and existing CLI/conformance behavior. Update this checklist only with implemented and tested
outcomes.

The Ruff plugin uses the sanitized-script engine ABI and is selected through
`--plugin`. Reimplementing workflow expression masking inside a second Rust
component would create competing sanitizer and source-mapping implementations.
The host chooses the ABI from the binary header, and each host validates its own
contract. Both plugin types carry the shared rule descriptor.

## Implementation status

`source.Reference` accompanies document facts, scripts, mappings, invocation sites and
findings. Nested composite callers have identities even when they contain no scripts.
`rule.Descriptor` separates category/subcategory from provider and maturity.
`checking.RulesWithPlugins` and `actionlint rules --plugin PATH` combine built-ins and selected
plugins in one collision-checked namespace. Registered document analyzers use
`lint.rules.plugins` for severity, options and presets; their option schemas are
validated without implicit file or network access. The catalogue generates `docs/rules.md`.

`traversal.Base` accumulates findings and supplies empty visitor callbacks.
Adapters request configuration and logging explicitly.
Source-local checks declare workflow, job and step callbacks through
`inputs.DeclarationCheck`. Their metadata, bindings, implementations and semantic
tests live under `internal/checking/rules/<category>`. Identifiers and dependencies
receive complete scopes without accumulating state through callback order.
`internal/checking/executor/declarations` invokes those callbacks and attaches
finding ownership. It also projects options for the commit-pinning, timeout and
required-permissions adapters.
Policy options are projected at construction; the check implementations do not load
repository configuration. Reusable-workflow metadata and permissions live in
`internal/model/calls`, and evaluated field shapes in `internal/model/workflows/schema`.
Expression, action-metadata, matrix and cache checks use visitor implementations.
They must be migrated with their context/suppression semantics covered; moving their files
alone does not complete that work. Plugin checks do not implement host visitor callbacks.

`internal/model/contexts/outputs` constructs the output types shared by `steps`,
`needs` and `jobs`. It receives published declarations and loaded dependency
metadata; it does not resolve references, read files or report findings. Known
output declarations constrain names. Unavailable or incomplete declarations leave
names open while preserving the string value type described by
[GitHub's context reference](https://docs.github.com/en/actions/reference/workflows-and-actions/contexts#jobs-context).
The expression checker loads metadata, supplies it to that model and checks uses
of the resulting types. `needs` includes direct prerequisites only.

`actionlint fix` previews revision-bound edits; `--apply` writes and reanalyzes them.
`actionlint lsp` uses versioned buffers, UTF-16 positions, cancellation, stale-publication
rejection and versioned code actions. It reanalyzes open documents rather than claiming a
fine-grained incremental semantic graph. Related locations in closed documents are not
published until those sources are available to the editor adapter.

Document-check components export `kjanat:actionlint/analyzer@0.2.0`. Core-module
plugins use their versioned JSON ABI. Script engines and runner providers implement
their own interfaces defined in [`plugins/protocol/wit`](../../plugins/protocol/wit).
The Go component host uses the maintained `github.com/kjanat/wacogo` fork.
The [Go fork inventory](../go-forks.md) records selected tags, recursive dependencies
and upstream origins.

The commitish provider is an optional process plugin with a versioned JSON contract.
It resolves action and reusable-workflow references to immutable identities with evidence;
it does not fetch dependency contents for recursive analysis or execute analysis remotely.
Caching is scoped to one analysis invocation. Credentials require an explicit provider
argument. Native providers use the same result validation as process providers.

Validation covers the full Go suite, unchanged 1,721-case conformance observations,
architecture dependency tests, fix conflicts/revisions, framed LSP exchanges and concurrent
cancellation, component fixtures from Rust/C/TinyGo, and the real Ruff artifact in the CLI,
Deno, wazero and Wasmi hosts. The commitish provider is compiled and tested offline; live
GitHub resolution has not been exercised as part of this change.

## Expression findings and webhook observations

The expression checker is a producer of several independently configurable rules.
Its traversal is not the diagnostic identity. Each production site supplies both
`Rule` (the configurable check) and `Code` (the diagnostic kind within that rule).
Messages and source positions do not determine either identity.

| Rule                   | Responsibility                                              | Example code              |
| ---------------------- | ----------------------------------------------------------- | ------------------------- |
| `expression-syntax`    | Tokens and expression grammar                               | `invalid-syntax`          |
| `expression-reference` | Names, properties, functions and declared variables/secrets | `unknown-variable`        |
| `context-availability` | Contexts and functions allowed at a workflow field          | `unavailable-context`     |
| `expression-type`      | Operand and argument compatibility                          | `argument-type`           |
| `expression-call`      | Call syntax, argument counts and constant arguments         | `argument-count`          |
| `event-payload`        | Known properties of selected event payload variants         | `unknown-property`        |
| `script-injection`     | Untrusted script interpolation                              | `untrusted-interpolation` |
| `expression`           | Workflow-field value and interpolation requirements         | —                         |

These rules have separate severity, preset and suppression settings. Disabling
`expression` no longer disables the independently named checks. `script-injection`
belongs to the security group; the other rules above belong to correctness.
JSON carries rule and code separately. LSP qualifies diagnostic codes with their
rule; SARIF uses `ruleId` for the rule and `properties.diagnosticCode` for the code.
The latter replaces the old external-only `externalCode` property.

Webhook observations are independent of this taxonomy. `internal/model/contexts/events/payload`
owns the generated graph and its provenance, and returns isolated data through
`Variants` and `Node`. It has no project-language dependencies. The workflow adapter
in `internal/model/contexts/events` owns trigger/activity selection, Actions aliases,
projection into expression types and Actions-specific overlays. Both checking and
editor completion consume that projection. The expression checker does not load
Octokit data, read configuration or perform network requests.

Unknown event variants allow arbitrary properties and branch guards do not yet refine payload
variants. Splitting diagnostic identities does not change those analysis limits.
The workflow visitor still supplies context and source mapping; migrating that
stateful traversal is separate from the identity and observation boundaries.
