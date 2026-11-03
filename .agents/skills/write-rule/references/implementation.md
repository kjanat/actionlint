# Implementation routes

Paths below are relative to the actionlint checkout root. They identify current
code to inspect, not a proposed package layout. Confirm signatures and callers
before editing; search by the named symbol if a file has moved.

## Native checks

For a declaration check, trace `missing-workflow-permissions` as a worked example:

| Responsibility                                           | Entry point                                                                                    |
| -------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| Public metadata and examples                             | `internal/rulecatalog/catalog.go`, `internal/rulecatalog/examples.go`                          |
| Selection, defaults, option type and configuration scope | `internal/config/catalog.go`                                                                   |
| Execution registration                                   | `internal/builtin/registry.go`                                                                 |
| Declaration adapter                                      | `internal/builtin/rule_declarations.go`                                                        |
| Check dispatch and semantic implementation               | `internal/workflow/checks/declarations.go`, `internal/workflow/checks/required_permissions.go` |
| Semantic regression cases                                | `internal/workflow/checks/missing_permissions_test.go`                                         |
| Configured host and suppression cases                    | `internal/integration/workflow/rule_missing_permissions_test.go`                               |

This example separates absent permissions from explicit `{}` and distinguishes a
reusable-only workflow from one with another trigger. Learn the separation of
concerns; do not copy its severity or compatibility settings to a new rule.

Use declaration views for source-local checks. For reasoning across calls, inspect
`internal/workflow/facts` and `internal/workflow/execution` and an existing consumer.
Shared semantic predicates live under `internal/semantics`. Adapters supply facts
and attach findings; they should not each reconstruct the same analysis.

For scalar values, inspect `StringView.Semantic()` in
`internal/workflow/declaration/views.go` and the value states and
`StringAlternatives()` in `internal/workflow/value/value.go`. `ContainsExpression()`
describes syntax, not whether a value is known: constant expressions can have
known results. Distinguish known alternatives from unresolved values using the
shared model; do not exempt every interpolation or implement another evaluator.

For action metadata or composite steps, also trace `internal/checks/registry.go`
(`Build`), `internal/builtin/composite_contract.go`, and
`internal/workflow/composition/analysis_action.go` and `composite_scripts.go`.
`SupportsActions` controls selection; it does not arrange callbacks.
`MetadataParticipant` and `InvocationParticipant` have different responsibilities.
Inspect their callers and a working participant before choosing one. Standalone
actions and actions reached through `uses` enter different lifecycles; both must
reach the check when the requested rule covers both. Check collection and source
admission as well as callback dispatch. Integration examples live in
`internal/integration/actions` and `internal/application/composite_analysis_test.go`.

`internal/finding`, `diagnostic`, and `source` carry findings, fixes, and source
identity. Trace the adapter through `internal/application/source_admission.go`
before attaching a finding to another file. Use existing span/source-map helpers
for decoded scalars; decoded string offsets are not YAML source offsets.

Inspect `rule/rule.go`, `rule/names.go`, and `internal/config/resolve_rules.go` for
descriptor, category, and selection contracts. Read current configuration examples
in `docs/config.md`. Public rule settings belong under
`lint.rules.<category>.<rule-id>`, beside that category's defaults. Legacy internal
fields and decode-only groups are not new public configuration surfaces.

## External linters and plugin checks

Choose an existing example matching the input and delivery contract:

- Process output plus jq: `examples/plugins/psscriptanalyzer/actionlint.plugin.ts`,
  `output.jq`, and `manifest.test.ts` in that directory.
- A TypeScript-authored script check with edits:
  `examples/plugins/typescript/actionlint.plugin.ts`.
- A whole-document check: `examples/plugins/external-linters/yamllint/actionlint.plugin.ts`.
- A component plugin: inspect a matching Rust example under `examples/plugins`
  and the WIT world it imports. Do not assume the process contract applies.

Read `packages/plugin-sdk/README.md` for authoring and packaging. Use the public
`@kjanat-actionlint/plugin-sdk` exports; workspace consumers declare `workspace:*`.
Test manifest schemas and jq using `createPluginTester` from the `/testing` export.
Its recorded-output cases exercise the host decoder without running the scanner.
Pass the intended actionlint executable explicitly when another version is on PATH.

Declare the scanner's actual input, accepted exit codes, coordinates, settings,
and capabilities. Preserve external diagnostic codes as codes rather than burying
them in message prefixes. Distinguish process failure from valid findings.
Recorded conversion tests do not prove installation or actual scanner execution.

## Renames and settings changes

Search the old ID across code, schemas, docs, fixtures, and examples. Inspect
`rule/names.go`, `internal/config/migration.go`, `internal/config/category_migration.go`,
`internal/suppression`, and `internal/diagnosticpolicy`. Check structured ignores,
inline selectors, unused-suppression accounting, CLI lookup, and emitted codes.

Use `internal/config/category_migration_test.go` for effective-selection comparison:
parse both versions, resolve under the same presets and registrations, compare
levels/options, then migrate again to prove idempotence. Do not replace legacy
group defaults with a wider semantic category default. Require plugin metadata
when needed to determine membership; do not guess from a plugin's name.
