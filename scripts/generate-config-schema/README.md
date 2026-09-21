# Configuration schemas

Run `go generate -run generate-config-schema` from the repository root to update
`actionlint.schema.json`. Actionlint owns tool selection, enablement, and the union
of an rc path and inline directives. The inline ShellCheck directives use a `$ref`
to `schemas/shellcheck/0.11.0.schema.json` through its GitHub raw URL.

The version identifies the ShellCheck directive contract represented by the YAML
mapping. This is an actionlint-maintained schema, not an upstream ShellCheck
schema or a requirement that users install exactly that ShellCheck version.
Runtime configuration parsing does not download schemas. Tests register the local
snapshot under its URL and disable external loading.

## Updating ShellCheck support

When a future ShellCheck version adds configuration directives:

1. Update the native directive type and parser.
2. Change `shellcheckSchemaVersion` in `main.go` to that ShellCheck version.
3. Run `go run ./scripts/generate-config-schema -init-shellcheck-schema` from the
   repository root to create the new snapshot. This command refuses to overwrite
   an existing version.
4. Regenerate the root schema, format both files, and run this package's tests.
5. Keep all previously published version files and URLs available.

Normal generation never rewrites the versioned snapshots. The snapshot test
detects drift between the current directive type and the selected version.
Corrections to descriptions or to an inaccurate existing contract require an
explicit edit and review; do not replace an old snapshot with a newer contract.
