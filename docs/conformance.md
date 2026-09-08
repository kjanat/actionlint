# Upstream conformance tests

The conformance job runs upstream fixtures through actionlint's Go implementation. It gives us independent inputs and
expectations before changing the implementation or attempting a language port.

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
go run ./scripts/fetch-conformance
go test -tags conformance -run '^TestUpstreamConformance' -count=1 -timeout 2m .
```

`make conformance` runs both commands. Ordinary `go test ./...` does not fetch or require these archives.

The fetch command downloads the revisions in [`sources.json`](../internal/conformance/sources.json) and verifies their
SHA-256 digests. Archives live in `.cache/conformance/`, outside Git. Tests read them without extracting or executing
upstream code. A missing or corrupt archive fails the test. Once the archives and Go dependencies are cached, the tests
run offline; no GitHub token, JavaScript runtime, .NET SDK, ShellCheck, or Pyflakes is needed.

Optional environment variables:

| Variable                          | Purpose                                                                                        |
| --------------------------------- | ---------------------------------------------------------------------------------------------- |
| `ACTIONLINT_CONFORMANCE_DIR`      | Read archives from another directory; pass the same directory to the fetch command with `-dir` |
| `ACTIONLINT_CONFORMANCE_REPORT`   | Write the full comparison as JSON to this path                                                 |
| `ACTIONLINT_CONFORMANCE_STRICT=1` | Fail on every upstream difference, including recorded differences                              |

CI runs on Linux, macOS, and Windows. Each job caches the archives, publishes a summary, and uploads its JSON report.

## What the assertions mean

- **Expressions:** compare lexing/parsing acceptance, context names, function names, and arity. The adapter uses
  actionlint's parser and semantic checker, with argument types erased for function-signature checks. It does not
  compare evaluated values or runtime coercion. An upstream evaluation error still means the expression parsed.
- **Workflow reader:** lint the original caller text with external tools disabled and an empty configuration. Extra
  lint findings count as differences. Remote reusable workflow bodies supplied to GitHub's mock file provider are
  not resolved by actionlint; the resulting differences are recorded explicitly. Expanded workflow objects and
  repository-specific limits are not compared.
- **Language service:** preserve the selected upstream assertions. The container tests check diagnostics mentioning
  `command` or `entrypoint`; the circular-alias case checks completion without a panic or hang. Other selected cases
  compare acceptance. Test blocks and assertions are read from the pinned TypeScript sources; unsupported changes to
  their shape fail the adapter.
- **Action manifests:** parse and validate metadata. Empty companion files satisfy the file-existence checks which
  the runner's manifest-parser fixtures do not exercise. Scripts and containers are never executed. Runtime
  deprecation diagnostics remain visible as additional lint findings.
- **YAML:** decode every document in each stream and compare success with the upstream `error` marker. This tests
  stream acceptance, not event sequences, scalar resolution, or GitHub's more restrictive YAML rules.

These are selected contracts from the upstream suites, not their complete test runners. Editor completion, hover,
protocol behavior, runner execution, and expression evaluation are outside this harness. An acceptance match on a
negative case means both implementations report an error; it does not prove they identify the same error. Existing
actionlint tests continue to check exact diagnostics and positions.

## Known differences

The initial comparison has **1,721 cases: 1,368 agreements and 353 differences**. That is a starting inventory, not a
compatibility score. Several fixtures exercise the same missing feature, and some differences are intentional lint
checks or limits of offline analysis.

[`differences.json`](../testdata/conformance/differences.json) groups exact case IDs and current diagnostics under an
explanation. Every listed case still runs. CI fails if a new difference appears, recorded diagnostics change, a known
difference disappears, or a recorded case is missing. There are no wildcard exceptions or automatic baseline updates.

Examples worth investigating include bracket wildcards, numeric expression spellings, expression depth limits,
Docker `pre-if`/`post-if`, Docker environment contexts, action output metadata, and reserved job ID prefixes. An
upstream reader fixture can describe internal or experimental behavior, so confirm the intended public contract
before changing actionlint to match it.

When fixing a difference, add a focused actionlint regression test and remove the resolved entry. If a diagnostic
changes intentionally, review and update its recorded text. Use strict mode to inspect the remaining disagreements.

## Updating the sources

The revisions keep a normal PR run reproducible. They pin test inputs; they do not control generated action/runtime
metadata or fetch anything while users run actionlint.

To update a source, resolve the desired commit in its repository (`main` for GitHub's repositories, `master` for SchemaStore,
`data` for YAML Test Suite), download its `https://codeload.github.com/OWNER/REPO/zip/COMMIT` archive, and calculate the
SHA-256 digest. Update its revision and digest together in `sources.json`, then run the fetch command and tests.

Review changed upstream assertions, fixture counts, adapter formats, and each new or resolved difference. The adapter
checks exact inventory counts so a changed fixture layout cannot silently reduce coverage. Update this document when
the selected contracts change. Never refresh the difference file merely to make CI green.

The upstream projects retain authorship of their fixtures. The cache keeps their archives unmodified, including any
license notices they contain; fixture source text is not copied into this repository or release packages.
