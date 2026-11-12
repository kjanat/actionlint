# Validate the rule

Run commands from the checkout root through `mise exec -- ...`. Select tests for
the changed behavior. Use repository instructions for expensive or external runs.

Read `tasks.toml`, `.mise.toml`, and the configuration for the tools you will run before choosing
commands. The `format` task runs `kprint fmt`, which reads `dprint.toml`; its configured
plugins include Go formatting through `gofumpt`. Use that repository pipeline,
selecting the changed files when formatting a limited change. A language formatter invoked
on its own does not replace the configured pipeline. Find the checks and generators
in the task definitions and source directives.

## Before implementation

Turn the original case into a regression test. Pair it with a similar input that
should not produce the finding. When the answer depends on a runtime value,
test an unknown value too and identify the information needed to decide whether
that input violates the rule.

For example, a cancellation check must distinguish job and step conditions and
intentional cleanup. See `internal/checking/rules/suspicious/uncancellable_condition_test.go`:
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

Choose test inputs from the behavior the rule promises to detect, the contexts
where it runs, and the interfaces changed by the implementation. Test through
actionlint as well as calling the check directly. Describe what each test verifies;
do not report untested behavior as verified.

For SDK authoring, `plugins/sdk/typescript/scripts/test.ts` runs tests against the
TypeScript source. Unless `ACTIONLINT` is set, it builds actionlint from this
checkout in a temporary directory and removes the temporary files after testing.
To test another actionlint version, set `ACTIONLINT` to its executable path:

```sh
mise exec -- bun plugins/sdk/typescript/scripts/test.ts tests/authoring.test.ts
```

## Independent and real-world checks

Use GitHub documentation or observed server responses for behavior controlled by
GitHub's server. For runner parsing or evaluation, inspect the runner implementation
or execute Runny at a recorded revision. For a plugin's output conversion, compare
against output from the scanner version it supports. Record the documentation URL
or implementation revision and what it confirms. Another linter or SchemaStore can reveal a
disagreement but cannot alone establish execution semantics.

For source-mapping comparisons, inspect
`internal/development/tests/conformance/yaml/scalar_source_reference_test.go` and
`internal/development/tests/conformance/yaml/testdata/scalar-source/reference.json`. The recorded test runs offline;
the `scalar-source-reference` task in `tasks.toml` also includes live Runny replay.
Do not present recorded replay as a fresh live comparison.

For the real-world comparison, choose a fixture copied from an existing workflow
or action that uses the syntax or behavior being checked. Record its original
repository and revision, review its expected findings, and run it through actionlint
with the changed rule. Synthetic integration fixtures test implementation behavior
but do not substitute for this comparison.
Explicitly select an opt-in rule for this comparison; a default run with that rule
disabled says nothing about its behavior.
Compare against the previous behavior and explain each new or removed finding.
If no real-world fixture covers that syntax or behavior, obtain an example from a
public repository and record its revision. Do not commit fixtures copied from a
private repository to this public repository or disclose their contents in public
reports. Parsing a workflow does not require executing its commands.

Save a minimal counterexample when a reference disagrees. Resolve whether the
difference is a bug, a version mismatch, or behavior the reference does not cover
before expanding what the rule reports. If the reference cannot be accessed or
run, state which behavior could not be verified.

## Final audit

Find and regenerate the derived files affected by the change. Inspect the result
and use the repository's checks for those artifacts.

Check generated completion and rule descriptions against the loader and emitted
diagnostic. For installed plugins, test the runtime schema as well as the static
one. Invoke the changed behavior through the CLI, LSP, or plugin interface that
users access. Separate test results, source inspection, captured reference
results, and live comparisons in the completion report.
