# Historical provider comparisons

`fixtures.json` records results from the former ShellCheck and Ruff adapters.
`fixtures/testdata/` contains the workflow inputs and local dependencies from
commit `51173cec`. All 247 workflow digests match the captured revisions in
`fixtures.json`.

The test runs from `fixtures/` to reproduce the original `testdata/` paths.
These inputs belong to this historical comparison. Current workflow tests use
their own corpus; adding or editing those tests does not change this baseline.

`cases.json` records the separate cases constructed in
`TestProviderParityCases`, including local actions, configuration and sourced
scripts.
