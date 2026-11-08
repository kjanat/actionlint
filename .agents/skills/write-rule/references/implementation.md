# Implementation routes

Paths below are relative to the actionlint checkout root. Confirm signatures and callers
before editing; search by the named symbol if a file has moved.

## Native checks

For a declaration check, trace `missing-workflow-permissions` as a worked example:

| Responsibility                                          | Entry point                                                                           |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| Rule metadata, examples and requested declaration views | `internal/checking/rules/security/missing-workflow-permissions.go`                    |
| Configuration projection                                | `internal/input/configuration/registry.go`, `internal/input/configuration/catalog.go` |
| Execution registration                                  | `internal/checking/executor/registration/registry.go`                                 |
| Declaration adapter                                     | `internal/checking/executor/declarations/visitor.go`                                  |
| Typed workflow, job and step inputs                     | `internal/checking/inputs/declarations.go`                                            |
| Semantic implementation                                 | `internal/checking/rules/security/permissions-checks.go`                              |
| Semantic regression cases                               | `internal/checking/rules/security/missing_workflow_permissions_test.go`               |
| Configured host and suppression cases                   | `internal/development/tests/integration/workflow/rule_missing_permissions_test.go`    |

This example separates absent permissions from explicit `{}` and distinguishes a
reusable-only workflow from one with another trigger. Follow its registration,
configuration, and diagnostic flow. Choose severity and compatibility behavior
for the new rule based on the problem it detects.

For source-local checks, return a `DeclarationCheck` with callbacks for the
workflow, job or step views the rule consumes. Leave other callbacks nil. Define
that binding beside the rule's metadata and implementation; the traversal adapter
does not select implementations by rule name. For reasoning across calls, inspect
`internal/model/workflows/facts` and `internal/model/workflows/execution` and an existing consumer.
Shared semantic predicates live under `internal/checking/constraints`. Adapters supply facts
and attach findings; they should not each reconstruct the same analysis.

Other parts of the model and host are relevant depending on the rule:

| Responsibility                        | Entry point                                                  |
| ------------------------------------- | ------------------------------------------------------------ |
| Declaration views and semantic values | `internal/model/workflows`, `internal/model/contexts/values` |
| Configured check selection            | `internal/checking/selection/registry.go`                    |
| Action participation contracts        | `internal/checking/executor/calls/composite_contract.go`     |
| Workflow and action execution         | `internal/checking/executor/composites`                      |
| Configured action integration tests   | `internal/development/tests/integration/actions`             |

For each fact the rule uses, read where it is produced and how existing checks
interpret it. Check which inputs produce a known value, an unknown value, or an
error before deciding when to report a finding.

`internal/results/finding`, `internal/results`, and `internal/input/source` carry findings, fixes, and source
identity. Trace the adapter through `internal/checking/source_admission.go`
before attaching a finding to another file. Use existing span/source-map helpers
for decoded scalars; decoded string offsets are not YAML source offsets.

Inspect `internal/checking/rules/declaration.go`, `internal/checking/rules/aliases.go`, and `internal/input/configuration/resolve_rules.go` for
descriptor, category, and selection contracts. Read current configuration examples
in `docs/config.md`. Public rule settings belong under
`lint.rules.<category>.<rule-id>`, beside that category's defaults. Legacy internal
fields and groups accepted only for migration must not be used as settings for new rules.

## External linters and plugin checks

Choose an existing example matching the input and delivery contract:

- Process output plus jq: `plugins/examples/psscriptanalyzer/actionlint.plugin.ts`,
  `output.jq`, and `manifest.test.ts` in that directory.
- A TypeScript-authored script check with edits:
  `plugins/examples/typescript/actionlint.plugin.ts`.
- A whole-document check: `plugins/examples/external-linters/yamllint/actionlint.plugin.ts`.
- A component plugin: inspect a matching Rust example under `plugins/examples`
  and the WIT world it imports. Do not assume the process contract applies.

Read `plugins/sdk/typescript/README.md` for authoring and packaging. Use the public
`@kjanat-actionlint/plugin-sdk` exports; workspace consumers declare `workspace:*`.
Test manifest schemas and jq using `createPluginTester` from the `/testing` export.
Its recorded-output cases pass saved scanner output to actionlint's decoder without running the scanner.
Pass the intended actionlint executable explicitly when another version is on PATH.

Declare the scanner's input format, accepted exit codes, line and column conventions,
settings, and capabilities. Return the scanner's diagnostic code in the code field.
Distinguish process failure from valid findings.
To verify installation and execution, install the plugin and run its scanner
through actionlint; recorded-output tests do not perform those operations.

## Renames and settings changes

Search the old ID across code, schemas, docs, fixtures, and examples. Inspect
`internal/checking/rules/aliases.go`, `internal/input/configuration/migration.go`, `internal/input/configuration/category_migration.go`,
`internal/results/suppression`, and `internal/results/policy`. Check structured ignores,
inline selectors, unused-suppression accounting, CLI lookup, and emitted codes.

Use `internal/input/configuration/category_migration_test.go` for effective-selection comparison:
parse both versions, resolve under the same presets and registrations, compare
levels/options, then migrate again to prove idempotence. Do not replace legacy
group defaults with a wider semantic category default. Require plugin metadata
when needed to determine membership; do not guess from a plugin's name.
