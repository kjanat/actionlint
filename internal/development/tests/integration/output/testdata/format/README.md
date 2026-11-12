These files test actionlint output formats in `workflow_render_test.go`.

How to generate `test.sarif`:

```sh
go test ./internal/development/tests/integration/output -run '^TestWorkflowDiagnosticSARIF$' -args -update-sarif
```

To generate the other files, run these commands from
`internal/development/tests/integration/output` with an `actionlint` binary
available on `PATH`. Run the template tests afterward to check the results.

````sh
actionlint --no-config --no-tool shellcheck --no-tool ruff -format '{{json .}}' testdata/format/test.yaml > testdata/format/test.json
actionlint --no-config --no-tool shellcheck --no-tool ruff -format '{{range $err := .}}{{json $err}}{{end}}' testdata/format/test.yaml > testdata/format/test.jsonl
actionlint --no-config --no-tool shellcheck --no-tool ruff -format '{{range $ := .}}### Error at line {{$.Line}}, col {{$.Column}} of `{{$.Filepath}}`\n\n{{$.Message}}\n\n```\n{{$.Snippet}}\n```\n\n{{end}}' testdata/format/test.yaml > testdata/format/test.md
````
