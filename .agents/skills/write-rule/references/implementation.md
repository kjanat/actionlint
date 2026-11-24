# Implementation routes

## Native checks

Trace `missing-permissions` as a worked example:

| Responsibility                        | Entry point                                                                                                    |
| ------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| Rule definition and implementation    | `MissingPermissionsRule` and `MissingPermissions` in `internal/checking/rules/security/missing-permissions.go` |
| Catalog order                         | `All` in `internal/checking/executor/registration/registry.go`                                                 |
| Rule type and adapter                 | `internal/checking/define/rule.go`, `internal/checking/define/checks.go`                                       |
| Semantic regression cases             | `internal/checking/rules/security/missing_workflow_permissions_test.go`                                        |
| Configured host and suppression cases | `internal/development/tests/integration/workflow/rule_missing_permissions_test.go`                             |

Define the rule once as a package-level `define.Rule` named `<Name>Rule`. Set `Name`, `Description`, `Category`, `Subcategory`, `Help` and any non-default `Recommended`, `DefaultLevel`, `Examples` or `RequiredInputs`. Declare options with `define.Options[T]()`, restrict when the rule runs with `Enabled`, and acquire inputs before construction with `Prepare`.

Choose exactly one way to run the rule:

- `Checks` for source-local checks. Return `define.Checks` with callbacks for the views in use and nil for the others. Use `define.Fixed` when the checks need no inputs.
- `Walker` for a stateful traversal over a `workflowvisit.Base`.
- `Script` for analysis of document scripts.
- `DeclaredBy` alone when another component reports the rule's findings.

Trace `internal/checking/source_admission.go` before attaching a finding to another file.

## External linters and plugin checks

Read the example that matches your plugin:

- Process output plus jq: `plugins/examples/psscriptanalyzer/`.
- TypeScript script check with edits: `plugins/examples/typescript/actionlint.plugin.ts`.
- Whole-document check: `plugins/examples/external-linters/yamllint/actionlint.plugin.ts`.

A script analyzer such as ShellCheck runs either as a process through `internal/checking/scriptanalysis/backends` or as a component plugin. A change to one route reaches only that route. A `Loaded component` line from `actionlint check --log-level debug` shows that the default configuration uses the component plugin. Test the change on the route in use.

Use the public `@kjanat-actionlint/plugin-sdk` exports. Workspace consumers declare `workspace:*`. Test manifest schemas and jq with `createPluginTester` from the `/testing` export.

## Renames and settings changes

Inspect `internal/model/rule/aliases.go`, `internal/input/configuration/migration.go`, `internal/input/configuration/category_migration.go`, `internal/results/suppression` and `internal/results/policy`. Check structured ignores, inline selectors, unused-suppression accounting, CLI lookup and emitted codes.

In `internal/input/configuration/category_binding_test.go`, resolve both versions under the same presets and registrations, compare levels and options, and migrate again to prove idempotence.
