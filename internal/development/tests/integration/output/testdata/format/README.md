These files test actionlint output formats in `workflow_render_test.go`.

The SARIF test validates output against the OASIS schema and checks the findings
and source locations from `test.yaml`.

To generate the template output files, run these commands from
`internal/development/tests/integration/output` with an `actionlint` binary
available on `PATH`. Run the template tests afterward to check the results.

````sh
actionlint --no-config --no-tool shellcheck --no-tool ruff -format '{{json .}}' testdata/format/test.yaml > testdata/format/test.json
actionlint --no-config --no-tool shellcheck --no-tool ruff -format '{{range $err := .}}{{json $err}}{{end}}' testdata/format/test.yaml > testdata/format/test.jsonl
actionlint --no-config --no-tool shellcheck --no-tool ruff -format '{{range $ := .}}### Error at line {{$.Line}}, col {{$.Column}} of `{{$.Filepath}}`\n\n{{$.Message}}\n\n```\n{{$.Snippet}}\n```\n\n{{end}}' testdata/format/test.yaml > testdata/format/test.md
````
