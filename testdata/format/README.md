Files in this directory are used for testing `-format` option of actionlint. Test cases are in `linter_test.go`.

How to generate `test.sarif`:

```sh
cd /path/to/actionlint

go run ./cmd/actionlint -no-config -shellcheck= --output-format sarif testdata/format/test.yaml > testdata/format/test.sarif

# Unit tests report the module version as (devel)
sed -i 's/"version": "v[^"]*"/"version": "(devel)"/' testdata/format/test.sarif
```

How to generate other files:

```sh
./actionlint -shellcheck= -format '{{json .}}' testdata/format/test.yaml > testdata/format/test.json
./actionlint -shellcheck= -format '{{range $err := .}}{{json $err}}{{end}}' testdata/format/test.yaml > testdata/format/test.jsonl
./actionlint -shellcheck= -format '{{range $ := .}}### Error at line {{$.Line}}, col {{$.Column}} of `{{$.Filepath}}`\n\n{{$.Message}}\n\n```\n{{$.Snippet}}\n```\n\n{{end}}' testdata/format/test.yaml > testdata/format/test.md
```
