# Implementation routes

Paths below are relative to the actionlint checkout root. Read the current function
signatures and their callers before editing. Search for the symbol if its file has moved.

## Native checks

For a declaration check, trace `missing-permissions` as a worked example:

| Responsibility                                         | Entry point                                                                           |
| ------------------------------------------------------ | ------------------------------------------------------------------------------------- |
| Rule definition: metadata, options, enablement, checks | `MissingPermissionsRule` in `internal/checking/rules/security/missing-permissions.go` |
| Configuration projection                               | `internal/input/configuration/registry.go`, `internal/input/configuration/catalog.go` |
| Catalog order                                          | `All` in `internal/checking/executor/registration/registry.go`                        |
| Rule definition and adapter                            | `internal/checking/define/rule.go`, `internal/checking/define/checks.go`              |
| Semantic implementation                                | `MissingPermissions` in `internal/checking/rules/security/missing-permissions.go`     |
| Semantic regression cases                              | `internal/checking/rules/security/missing_workflow_permissions_test.go`               |
| Configured host and suppression cases                  | `internal/development/tests/integration/workflow/rule_missing_permissions_test.go`    |

This example separates absent permissions from explicit `{}` and distinguishes a
reusable-only workflow from one with another trigger. Follow its registration,
configuration, and diagnostic flow. Choose severity and compatibility behavior
for the new rule based on the problem it detects.

Define a rule once, as a package-level `define.Rule` named `<Name>Rule`. Set
`Name`, `Description`, `Category`, `Subcategory`, `Help` and any non-default
`Recommended`, `DefaultLevel`, `Examples` or `RequiredInputs`. Leave `Provider`,
`Maturity`, `Explanation` and `DocumentationURL` unset to take the defaults:
`actionlint`, stable, the description as a sentence and the rule's section in
`docs/rules.md`. Declare options with `define.Options[T]()`, restrict when the rule
runs with `Enabled`, and acquire inputs before construction with `Prepare`.

Choose exactly one way to run the rule:

- `Checks` for source-local checks. Return `define.Checks` with callbacks for the
  workflow, job or step views the rule consumes and leave the others nil; use
  `define.Fixed` when the checks need no inputs. The adapter attaches the rule's
  name to findings that leave `Rule` empty, so checks do not repeat it.
- `Walker` for a stateful traversal. It receives a `workflowvisit.Base` carrying the
  rule's name and description.
- `Script` for analysis of document scripts.
- None, with `DeclaredBy`, for a rule whose findings another component reports.

Add the rule to `All`; a test fails when a defined rule is missing or listed twice.
The traversal adapter does not select implementations by rule name. For reasoning across calls, inspect
`internal/model/workflows/facts` and `internal/model/workflows/execution` and an existing consumer.
Shared semantic predicates belong in the model packages, not in rule adapters. Adapters supply facts
and attach findings; they should not each reconstruct the same analysis.

Other parts of the model and host are relevant depending on the rule:

| Responsibility                        | Entry point                                                 |
| ------------------------------------- | ----------------------------------------------------------- |
| Declaration views and semantic values | `internal/model/workflows`, `internal/model/contexts/value` |
| Configured check selection            | `internal/checking/selection/registry.go`                   |
| Action participation contracts        | `internal/checking/workflowvisit/invocation_contract.go`    |
| Workflow and action execution         | `internal/checking/executor/composites`                     |
| Configured action integration tests   | `internal/development/tests/integration/actions`            |

For each fact the rule uses, read where it is produced and how existing checks
interpret it. Check which inputs produce a known value, an unknown value, or an
error before deciding when to report a finding.

`internal/results/finding`, `internal/results`, and `internal/input/source` carry findings, fixes, and source
identity. Trace the adapter through `internal/checking/source_admission.go`
before attaching a finding to another file. Use existing span/source-map helpers
for decoded scalars; decoded string offsets are not YAML source offsets.

Inspect `internal/model/rule/declaration.go`, `internal/model/rule/aliases.go`, and `internal/input/configuration/resolve_rules.go` for
rule metadata, categories and preset selection. Read current configuration examples
in `docs/config.md`. Public rule settings belong under
`lint.rules.<category>.<rule-id>`, beside that category's defaults. Legacy internal
fields and groups accepted only for migration must not be used as settings for new rules.

## External linters and plugin checks

Choose an example for the data your plugin checks and how actionlint runs it:

- Process output plus jq: `plugins/examples/psscriptanalyzer/actionlint.plugin.ts`,
  `output.jq`, and `manifest.test.ts` in that directory.
- A TypeScript-authored script check with edits:
  `plugins/examples/typescript/actionlint.plugin.ts`.
- A whole-document check: `plugins/examples/external-linters/yamllint/actionlint.plugin.ts`.
- A component plugin: inspect a matching Rust example under `plugins/examples`
  and the WIT world it imports. Component plugins exchange data through those WIT interfaces.

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
`internal/model/rule/aliases.go`, `internal/input/configuration/migration.go`, `internal/input/configuration/category_migration.go`,
`internal/results/suppression`, and `internal/results/policy`. Check structured ignores,
inline selectors, unused-suppression accounting, CLI lookup, and emitted codes.

Use `internal/input/configuration/category_binding_test.go` for effective-selection comparison:
parse both versions, resolve under the same presets and registrations, compare
levels/options, then migrate again to prove idempotence. Do not replace legacy
group defaults with a wider semantic category default. Require plugin metadata
when needed to determine membership; do not guess from a plugin's name.
