# Validate the rule

## Before implementation

Turn the original case into a regression test. Add a similar input that must not produce the finding. When the answer depends on a runtime value, test an unknown value too.

## During implementation

To run the SDK tests against another actionlint, set `ACTIONLINT` to its path:

```sh
mise exec -- bun plugins/sdk/typescript/scripts/test.ts tests/authoring.test.ts
```

## Independent and real-world checks

Confirm behavior with these sources:

- GitHub's server: GitHub documentation or observed server responses.
- Runner parsing or evaluation: the runner implementation, or Runny at a recorded revision.
- Plugin output conversion: output from a supported scanner version.

Record the URL or revision and what it confirms. Another linter or SchemaStore alone does not establish execution semantics.

`internal/development/tests/conformance/yaml/scalar_source_reference_test.go` replays recorded Runny output. Only the `scalar-source-reference` task compares source mapping with live Runny.

Copy a public workflow or action that uses the checked syntax and record its repository and revision. Select the changed rule explicitly. Build the previous version with `git archive <base> | tar -x -C <scratch directory>`, run the copy through both executables, and explain each new or removed finding. Do not commit or disclose fixtures from private repositories.

Save a minimal counterexample when a reference disagrees. Classify the disagreement as a bug, a version mismatch or a coverage gap before expanding the rule. When a change stops reporting values, run each value through the reference. If the reference reports a value, keep reporting it or give it its own finding ID. State which behavior you could not verify.

## Final audit

In the completion report, separate test results, source inspection, recorded reference results and live comparisons.
