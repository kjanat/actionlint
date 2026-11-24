# Upstream conformance tests

The conformance job runs fixtures from reference projects through actionlint and compares the results with recorded expectations. Workflow fixtures run with the recommended correctness and suspicious checks.

## Run locally

```sh
go run ./internal/development/tests/conformance/fetch
go test -tags conformance -run '^TestUpstreamConformance' -count=1 -timeout 2m ./internal/development/tests/conformance
```

The fetch command downloads the revisions in [`sources.json`](../internal/development/tests/conformance/sources.json) to `.cache/conformance/` and verifies their SHA-256 digests. A missing or corrupt archive fails the test. With the archives cached, the tests run offline.

| Variable                          | Purpose                                                                                        |
| --------------------------------- | ---------------------------------------------------------------------------------------------- |
| `ACTIONLINT_CONFORMANCE_DIR`      | Read archives from another directory; pass the same directory to the fetch command with `-dir` |
| `ACTIONLINT_CONFORMANCE_REPORT`   | Write observations here and a separate review report at `<path>.assessments.json`              |
| `ACTIONLINT_CONFORMANCE_STRICT=1` | Fail on every upstream difference, including recorded differences                              |

Pass `-v` to `go test` to print the assessment inventory and comparison totals.

## Observations, regression expectations, and assessments

[`differences.json`](../internal/development/tests/conformance/testdata/differences.json) maps case IDs to their exact actionlint diagnostics.

[`assessments.json`](../internal/development/tests/conformance/testdata/assessments.json) groups case IDs with observation fingerprints and reviewed findings.

| Assessment            | Meaning                                                                           |
| --------------------- | --------------------------------------------------------------------------------- |
| `actionlint-gap`      | Actionlint lacks supported behavior or required validation.                       |
| `reference-gap`       | The reference project rejects working syntax or lacks required validation.        |
| `extra-lint`          | Actionlint deliberately checks lint policy beyond the reference's contract.       |
| `adapter-limit`       | The comparison adapter cannot reproduce part of the reference's test environment. |
| `dependency-gap`      | The YAML library disagrees with the YAML stream specification fixtures.           |
| `contract-difference` | The compared entry points intentionally have different input contracts.           |
| `unassessed`          | A review records an unresolved question without attributing the difference.       |

A review becomes `stale` when its observation changes and `resolved` when the case now agrees. Neither counts toward a category, and neither fails the test.

When you fix a difference, add a focused regression test, then remove its baseline entry and assessment.

For an intentional diagnostic change, update the baseline and review each finding before you copy the new fingerprint. The harness never refreshes fingerprints.

## Updating the sources

Download `https://codeload.github.com/OWNER/REPO/zip/COMMIT` and compute its SHA-256 digest. Update the revision and digest together in `sources.json`, then run the fetch command and the tests. A new revision makes the affected assessments stale. Review their evidence before you update the fingerprints.
