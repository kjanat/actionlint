# Upstream conformance tests

The conformance job runs reference-project fixtures through actionlint's Go implementation. It compares the recorded
contracts and keeps score of reviewed differences. Reference expectations can reflect missing features or validation.
Assessments use implementation or runtime evidence to explain each difference.

For live GitHub expression evaluation, coercion and `fromJSON()` behavior, see the separate
[expression reference and reproduction workflow](expression-behavior.md).

## Sources and coverage

| Source                                                                  | Imported cases | What is compared                                                                                  |
| ----------------------------------------------------------------------- | -------------: | ------------------------------------------------------------------------------------------------- |
| [actions/languageservices](https://github.com/actions/languageservices) |          1,228 | 1,020 expression fixtures, 195 workflow-reader fixtures, and 13 language-service validation cases |
| [actions/runner](https://github.com/actions/runner)                     |             24 | 20 action-manifest fixtures and four expression context/function tests                            |
| [SchemaStore/schemastore](https://github.com/SchemaStore/schemastore)   |             67 | Positive and negative GitHub workflow/action schema fixtures                                      |
| [yaml/yaml-test-suite](https://github.com/yaml/yaml-test-suite)         |            402 | YAML stream acceptance by the Go YAML dependency                                                  |

GitHub's language server lives in `actions/languageservices`. Its shared expression and workflow-parser corpora are
designed for use across languages. We also import the language-service tests for service container commands and YAML
anchors. The runner fixtures come from `ActionManifestManagerL0.cs` and `ExpressionParserL0.cs`.

SchemaStore and YAML Test Suite provide additional comparisons. They do not define GitHub's runtime behavior. In
particular, a schema-valid example can still contain undefined context properties, incomplete jobs, or retired actions.

## Run locally

```sh
go run ./internal/conformance/fetch
go test -tags conformance -run '^TestUpstreamConformance' -count=1 -timeout 2m ./internal/conformance
```

`make conformance` runs both commands. The harness, adapters, assessment logic, and baselines live together in
[`internal/conformance/`](../internal/conformance/). The harness uses the public actionlint APIs from an external test
package. Ordinary `go test ./...` does not fetch or require these archives.

Relative archive and report paths are resolved from the repository root, including when the tests run from the
`internal/conformance` package directory.

The fetch command downloads the revisions in [`sources.json`](../internal/conformance/sources.json) and verifies their
SHA-256 digests. Archives live in `.cache/conformance/`, outside Git. Tests read them without extracting or executing
upstream code. A missing or corrupt archive fails the test. Once the archives and Go dependencies are cached, the tests
run offline; no GitHub token, JavaScript runtime, .NET SDK, ShellCheck, or Ruff is needed.

Optional environment variables:

| Variable                          | Purpose                                                                                        |
| --------------------------------- | ---------------------------------------------------------------------------------------------- |
| `ACTIONLINT_CONFORMANCE_DIR`      | Read archives from another directory; pass the same directory to the fetch command with `-dir` |
| `ACTIONLINT_CONFORMANCE_REPORT`   | Write observations here and a separate review report at `<path>.assessments.json`              |
| `ACTIONLINT_CONFORMANCE_STRICT=1` | Fail on every upstream difference, including recorded differences                              |

CI runs on Linux, macOS, and Windows. Each job caches the archives, publishes a summary, and uploads both reports.

The observation report contains pinned source revisions, case IDs, fixture input hashes, reference expectations,
reference skip metadata, the comparison contract, and actionlint diagnostics. It contains no judgments or scores.
The companion assessment report joins those observations to separately reviewed findings and provides the derived
score. The summary keeps reference comparison counts and reviewed assessments in separate tables.

The summary also includes a collapsible assessment inventory: group counts per category, then each group's index in
`assessments.json`, case count, review status, finding scope, and rationale. Mixed groups list each finding separately.
Pass `-v` to `go test` to print the same inventory after all test results, followed by the final comparison totals.
Stale and resolved reviews remain visible with their status; only current reviews contribute to the scored case and
finding counts.

## What the assertions mean

- **Expressions:** compare lexing/parsing acceptance, context names, function names, and arity. The adapter uses
  actionlint's parser and semantic checker, with argument types erased for function-signature checks. It does not
  compare evaluated values or runtime coercion. An upstream evaluation error still means the expression parsed.
- **Workflow reader:** lint the original caller text with external tools disabled and an empty configuration. Extra
  lint findings count as differences. Remote reusable workflow bodies supplied to GitHub's mock file provider are
  not resolved by actionlint; the resulting differences are recorded explicitly. Expanded workflow objects and
  repository-specific limits are not compared. Fixture `skip` metadata is retained as `reference_skips` in the JSON
  report. A shared fixture skipped by TypeScript does not establish TypeScript support; actionlint still runs it.
- **Language service:** preserve the selected upstream assertions. The container tests check diagnostics mentioning
  `command` or `entrypoint`; the circular-alias case checks completion without a panic or hang. Other selected cases
  compare acceptance. Test blocks and assertions are read from the pinned TypeScript sources; unsupported changes to
  their shape fail the adapter.
- **Action manifests:** parse and validate metadata. Empty companion files satisfy the file-existence checks which
  the runner's manifest-parser fixtures do not exercise. Scripts and containers are never executed. Runtime
  deprecation diagnostics remain visible as additional lint findings.
- **YAML:** decode every document in each stream and compare success with the upstream `error` marker. The assertion
  covers stream acceptance. Event sequences, scalar resolution, and GitHub's more restrictive YAML rules are outside
  its scope.

The harness exercises the upstream contracts listed above. Editor completion, hover, protocol behavior, runner
execution, and expression evaluation are outside its scope. Matching rejection establishes that both implementations
report an error. They may report different errors; existing actionlint tests check exact diagnostics and positions.

## Observations, regression expectations, and assessments

Three distinct records serve different purposes:

- [`differences.json`](../internal/conformance/testdata/differences.json) is the diagnostic regression baseline: an exact map
  of case IDs to actionlint diagnostics. It contains no explanations or classifications. Every case still runs.
  CI fails when a new difference appears, diagnostics change, a difference resolves, or a baseline case disappears.
- The generated observation report records what was compared and what happened, including every agreement.
  It is written before assessments are loaded, so a malformed review does not prevent observation output.
- [`assessments.json`](../internal/conformance/testdata/assessments.json) contains reviewed interpretations. A group maps exact
  case IDs to observation fingerprints and lists one or more findings, each with a category, scope, rationale, and
  evidence references. This file has no diagnostic regression expectations.

Assessment scopes are `fixture`, `feature`, and `diagnostic`. A diagnostic finding selects zero-based indices from
that observation's `actual` array. Feature findings can describe behavior such as insertion support independently
of a fixture's unrelated diagnostics. Mixed fixtures can carry several findings and categories. Evidence may refer
to a pinned implementation, a recorded runtime probe, or `fixture:<exact-case-id>` in the pinned corpus. These are
reviewer-supplied evidence references. The harness validates the record structure; reviewers are responsible for
checking the evidence and reasoning.

| Assessment            | Meaning                                                                           |
| --------------------- | --------------------------------------------------------------------------------- |
| `actionlint-gap`      | Actionlint lacks supported behavior or required validation.                       |
| `reference-gap`       | The reference project rejects working syntax or lacks required validation.        |
| `extra-lint`          | Actionlint deliberately checks lint policy beyond the reference's contract.       |
| `adapter-limit`       | The comparison adapter cannot reproduce part of the reference's test environment. |
| `dependency-gap`      | The YAML library disagrees with the YAML stream specification fixtures.           |
| `contract-difference` | The compared entry points intentionally have different input contracts.           |
| `unassessed`          | A review records an unresolved question without attributing the difference.       |

The harness never infers fault from acceptance or rejection. Classified findings require evidence references;
`unassessed` findings still require a rationale. Unknown fields, categories, invalid scopes, duplicate case assignments,
unknown case IDs, and invalid diagnostic indices are rejected.

Each review is bound to a SHA-256 fingerprint of the pinned source metadata and the observation, including the input
hash, reference expectations, diagnostics, comparison contract, and skip metadata. Changed evidence makes the review
`stale`. Stale and missing reviews remain visible but contribute no category counts. A reviewed case that now agrees
is marked `resolved` and also contributes no category counts. These statuses are separate from `unassessed`, which is
an explicit unresolved judgment on a current observation. Staleness alone does not fail the test; baseline changes
still fail independently and must be reviewed.

The assessment report counts **distinct cases per category** and **findings per category** separately. A case with two
findings in one category counts once in that category's case column and twice in its findings column. A case spanning
two categories appears in both rows, so category totals are not additive fixture counts. The observation table remains
an exclusive count of agreements and differences. Reclassifying a review cannot change observations or weaken the
regression assertions. Strict mode still fails on every acceptance difference.

For example, the shared `actions/languageservices` reader fixture `insert.yml` uses a working mapping insertion and
explicitly skips TypeScript. Actionlint supports that directive and still diagnoses an undeclared `steps.set-matrix`
reference in the fixture. Its `reference-gap` assessment records the reference syntax-support gap; the shared expected
output is not evidence that the TypeScript reader supports insertion. A removed diagnostic still requires review of
its recorded expectation.

For the whitespace fixture, the lexer compatibility issue and retired-action diagnostic are separate findings.
The checked-in [service and runner probe results](expression-results/2026-09-14.md) establish support for the nonfinite
expression literals and several numeric spellings. Reference-only limits and nonfinite timeout validity are not
treated as proven GitHub runtime defects. Negative hexadecimal expressions and nonfinite workflow timeouts retain
explicit unresolved assessments. See the generated reports for current counts.

For composite outputs, the pinned runner schema permits an omitted `value`, and
[`CompositeActionHandler.ProcessOutputs()`](https://github.com/actions/runner/blob/602c0085328df8cb595fc2641d69f640a11377a4/src/Runner.Worker/Handlers/CompositeActionHandler.cs#L183)
sets an output only when that key exists. SchemaStore's stricter requirement is recorded as a contract difference.
For expression whitespace, the
[runner lexer](https://github.com/actions/runner/blob/602c0085328df8cb595fc2641d69f640a11377a4/src/Sdk/DTExpressions2/Expressions2/Tokens/LexicalAnalyzer.cs#L19)
uses `Char.IsWhiteSpace`, which includes U+00A0; actionlint's four-character whitespace set does not. That is an
actionlint compatibility gap against the runner implementation. The shared whitespace fixture skips TypeScript,
and its retired-action finding is independent of that gap.

Examples worth investigating include bracket wildcards, numeric expression spellings, expression depth limits,
Docker `pre-if`/`post-if`, Docker environment contexts, action output metadata, and reserved job ID prefixes. An
upstream reader fixture can describe internal or experimental behavior, so confirm the intended public contract
before changing actionlint to match it.

When fixing a difference, add a focused actionlint regression test and review the new observation. Remove a resolved
baseline entry and its assessment; for an intentional diagnostic change, update the baseline text and review each
finding against the new observation before copying its fingerprint into the assessment file. Fingerprints are never
refreshed automatically. Changing only a judgment or its rationale requires no baseline or observation edit. Use strict
mode to inspect the remaining acceptance disagreements.

## Updating the sources

The revisions keep a normal PR run reproducible. They pin test inputs; they do not control generated action/runtime
metadata or fetch anything while users run actionlint.

To update a source, resolve the desired commit in its repository (`main` for GitHub's repositories, `master` for SchemaStore,
`data` for YAML Test Suite), download its `https://codeload.github.com/OWNER/REPO/zip/COMMIT` archive, and calculate the
SHA-256 digest. Update its revision and digest together in `sources.json`, then run the fetch command and tests.

Review changed upstream assertions, fixture counts, adapter formats, and each new or resolved difference. The adapter
requires the recorded fixture counts to match the imported corpus. Review count changes when updating the sources,
and update this document when the selected contracts change. Source revision changes make the affected assessments
stale, even when diagnostics happen to stay the same. Review their evidence before updating their fingerprints.

The upstream projects retain authorship of their fixtures. The cache keeps their archives unmodified, including any
license notices they contain; fixture source text is not copied into this repository or release packages.
