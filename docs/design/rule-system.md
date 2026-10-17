# Unified rule system

Status: shared contracts and host workflows implemented; legacy semantic migration remains in progress.

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

- [ ] Complete the migration of remaining visitor-based semantic checks.

## Decisions

Input preparation, analysis and presentation remain separate. Public analyzers consume explicit
facts and return findings; the host owns scheduling, configuration selection,
resource lifetime, suppression and severity. Native built-ins and component plugins
share rule metadata and result semantics. Rule identity, provider identity and a
provider's execution backend are separate concepts.

A source reference identifies a document, a content revision, a node and its
structural address. Offsets are locations, not identity. Explicit job/step IDs are
preferred. Anonymous nodes are matched by structural content and occurrence; named nodes retain identity across editor revisions. Anonymous content edits
produce new identities; their structural address remains available for client reconciliation. Indistinguishable duplicate
nodes require an ordering tie-breaker.

The internal YAML package owns the tree consumed by workflow, mapping and engine semantics. Parser-specific types
are confined to decoding/encoding. Retaining the existing parser behind this
boundary avoids changing YAML acceptance at the same time as rule execution.

Fixes declare applicability and carry complete edit groups. Application checks
source revisions, ranges and overlaps before writing. Preview and LSP code actions
consume the same edit representation. Analysis runs again after a CLI application. All files are validated before writing, but atomic replacement is per file; an I/O failure can leave a partially applied multi-file change and reports the written files.

Remote resolution is an injected capability with explicit requests, immutable
resolved identities, cancellation and provenance. commitish supplies GitHub ref
semantics; the engine must not recreate those semantics or obtain credentials
implicitly. Network access remains opt-in.

Generated reference material belongs to the rule catalogue. Generator logic lives
with its owning subsystem; executable entry points remain thin. Release and
conformance tools retain their own tests and domain directories.

## Script-tool boundary

`internal/tools` defines registration, prepared script batches and findings.
`internal/tools/backend` adapts the shared snippet protocol to native processes or
an injected WASM backend. `internal/tools/adapters` owns the bundled native CLI
protocols, tool configuration, expression preparation and diagnostic conversion.
The engine and application depend on registrations, not these implementations.

`internal/analyzerhost` owns process capacity and completion. `internal/localapp`
composes artifact loading and registrations; independent core engines advertise
shell selectors in metadata. Adding a provider requires no engine-name switch.
Dependency tests enforce both directions, and integration tests exercise an
independent process provider and a remote core-WASM artifact.

## Validation

Verify contracts with native and WASM consumers, final artifacts, source-coordinate
and identity regressions, fix conflicts/staleness, LSP protocol exchanges, remote
provider failures, and existing CLI/conformance behavior. Keep architecture tests
for dependency direction. Update this checklist only with implemented and tested
outcomes.

The Ruff plugin retains the sanitized-script engine ABI and is selectable through
`--plugin`. Reimplementing workflow expression masking inside a second Rust
component would create competing sanitizer and source-mapping implementations.
The host chooses the ABI from the binary header, and each host validates its own
contract. Both plugin types carry the shared rule descriptor.

## Implemented boundaries and remaining work

`source.Reference` accompanies document facts, scripts, mappings, invocation sites and
findings. Nested composite callers have identities even when they contain no scripts.
`rule.Descriptor` separates category/subcategory from provider and maturity.
`analysis.Rules` and `actionlint rules --plugin PATH` combine built-ins and selected
plugins in one collision-checked namespace. Registered document analyzers use
`lint.rules.plugins` for severity, options and presets; their option schemas are
validated without implicit file or network access. The catalogue generates `docs/rules.md`.

`RuleBase` remains an internal visitor adapter and finding accumulator. It no longer
owns configuration or logging. Adapters opt into those capabilities explicitly.
Environment names, conditions, permission declarations, identifiers and job dependency
checks have stateless implementations in `internal/checks`; identifiers and dependencies
receive complete scopes, without accumulating state through callback order.
`internal/workflow/checks` also owns events, credentials, glob validation, parallel
steps and the required-actions, commit-pinning, timeout and required-permissions checks.
Policy options are projected at construction; the check implementations do not load
repository configuration. Reusable-workflow metadata and permissions live in
`internal/workflow/calls`, and evaluated field shapes in `internal/workflow/schema`.
Expression, action-metadata, matrix, cache, runner and shell checks still use visitor implementations.
They must be migrated with their context/suppression semantics covered; moving their files
alone does not complete that work. Public analyzers do not implement visitor callbacks.

`actionlint fix` previews revision-bound edits; `--apply` writes and reanalyzes them.
`actionlint lsp` uses versioned buffers, UTF-16 positions, cancellation, stale-publication
rejection and versioned code actions. It reanalyzes open documents rather than claiming a
fine-grained incremental semantic graph. Related locations in closed documents are not
published until those sources are available to the editor adapter.

The component contract is `kjanat:actionlint/analyzer@0.1.0`. The Ruff repository advertises the shared rule
metadata through its existing core-engine ABI 1, and its rebuilt artifact works through
the CLI's sanitizer and source mapper. Both loaders retain metadata and source provenance.
The Go component host uses the original `partite-ai/wacogo` dependency.

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
rule; SARIF retains `ruleId` and places the code in `properties.diagnosticCode`.
The latter replaces the old external-only `externalCode` property.

Webhook observations are independent of this taxonomy. `internal/webhook/payload`
owns the generated graph and its provenance, and returns isolated data through
`Variants` and `Node`. It has no project-language dependencies. The workflow adapter
in `internal/workflow/eventpayload` owns trigger/activity selection, Actions aliases,
projection into expression types and Actions-specific overlays. Both checking and
editor completion consume that projection. The expression checker does not load
Octokit data, read configuration or perform network requests.

Unknown event variants remain open and branch guards do not yet refine payload
variants. Splitting diagnostic identities does not change those analysis limits.
The workflow visitor still supplies context and source mapping; migrating that
remaining stateful traversal is separate from the identity and observation boundaries.
