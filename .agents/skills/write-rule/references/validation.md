# Validate the rule

Run commands from the checkout root through `mise exec -- ...`. Select tests for
the changed behavior; the examples below are starting points, not a mandatory
full-suite invocation. Use repository instructions for expensive/external runs.

## Before implementation

Turn the original case into a focused regression. Pair it with a valid near miss
that prevents an overbroad implementation. For runtime-dependent claims, add an
unknown case and state what evidence would be needed to decide it.

For example, a cancellation check must distinguish job and step conditions and
intentional cleanup. See `internal/workflow/checks/uncancellable_condition_test.go`:
it exercises compound conditions and classifies behavior-changing fixes as unsafe.
The existence of that test is not independent proof of GitHub's behavior.

## During implementation

| Changed behavior              | Evidence to collect                                                                                                                        |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| Semantic predicate            | Positive, valid negative, unknown/dynamic cases where applicable; exact diagnostic identity and source range                               |
| Invocation-sensitive analysis | Same declaration called with differing inputs or ancestry; findings attached to the right invocation                                       |
| Selection or options          | Default, explicit level/off, strict/experimental, inheritance and file overrides; invalid options rejected at binding                      |
| Suppression                   | Target finding suppressed; unrelated finding not suppressed; used selector not called unused                                               |
| Fix                           | Correct source revision/range; edited YAML parses; targeted finding addressed; unrelated bytes preserved; second application harmless      |
| Rename/migration              | Old selector compatibility; canonical output; effective levels/options unchanged across YAML/JSON/TOML; conflicts and repeated migration   |
| Plugin transform              | Recorded real stdout, empty output, failure exit, malformed output, Unicode coordinates, codes, settings and edits through host validation |

Run the semantic test, then the frontend test proving the real host path. Examples:

For an action-capable rule, exercise a workflow step, a standalone action file,
and a workflow invoking a composite action. Assert the target finding, its level,
and its source file in each positive case. A zero-finding result is not evidence
that a registered participant ran. Check suppressions and duplicate emission
where the same action is called more than once.

```sh
mise exec -- go test ./internal/workflow/checks -run '^TestMissingWorkflowPermissions$' -count=1
mise exec -- go test ./internal/integration/workflow -run '^TestMissingPermissionsSelectionAndSuppression$' -count=1
mise exec -- go test ./internal/config -run '^TestCategoryMigration' -count=1
```

For SDK authoring, the existing runner builds this checkout's host in a temporary
directory, runs TypeScript-source tests, and cleans up. Leave `ACTIONLINT` unset
to use that build, or set it deliberately to the host being tested:

```sh
mise exec -- bun packages/plugin-sdk/scripts/test.ts tests/authoring.test.ts
```

## Independent and real-world checks

Choose the reference for the claim: GitHub docs/server observations for hosted
behavior, pinned runner/Runny for runner parsing or evaluation, and the scanner's
own output for a provider transform. Another linter or SchemaStore can reveal a
disagreement but cannot alone establish execution semantics.

For source-mapping comparisons, inspect
`internal/workflow/parser/scalar_source_reference_test.go` and
`testdata/conformance/scalar-source/reference.json`. The recorded test runs offline;
the `scalar-source-reference` task in `tasks.toml` also includes live Runny replay.
Do not present recorded replay as a fresh live comparison.

Take a relevant workflow/action from `internal/integration` or `testdata`. Inspect
its provenance and expected findings, then run the changed rule through the host.
Explicitly select an opt-in rule for this comparison; a default run with that rule
disabled says nothing about its behavior.
Compare against the previous behavior and explain each new or removed finding.
If no useful case exists, obtain a public, revision-pinned example. Keep private
fixtures private. Parsing a workflow does not require executing its commands.

Save a minimal counterexample when a reference disagrees. Resolve whether the
difference is a bug, version mismatch, or unsupported evidence before broadening
the rule. Report an unavailable reference honestly instead of inventing certainty.

## Final audit

When catalogue/schema inputs changed, regenerate and inspect:

```sh
mise exec -- go run ./internal/rulecatalog/generate
mise exec -- go run ./tools/config
mise exec -- go test ./internal/rulecatalog ./tools/config ./internal/editorschema
```

`TestGeneratedSchemaUpToDate` in `tools/config` checks the generated configuration
schema; generating only `docs/rules.md` does not update it.

Check generated completion and rule descriptions against the loader and emitted
diagnostic. For installed plugins, test the runtime schema as well as the static
one. Review the actual CLI/LSP or provider route affected by the change, not only
the internal helper. Separate test results, source inspection, captured reference
results, and live comparisons in the completion report.
