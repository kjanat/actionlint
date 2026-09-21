# Configuration schemas

Run `go generate -run generate-config-schema` from the repository root to update
`actionlint.schema.json`. Actionlint owns tool selection, enablement, and the union
of an rc path and inline directives. The inline ShellCheck directives use a `$ref`
to `schemas/shellcheck/0.11.0.schema.json`, relative to the main schema's location.
Both schemas omit `$id` so their retrieval location determines reference resolution.
An installed npm package uses its bundled schema; a versioned CDN URL or Git
commit URL uses the schema from that same release or revision.

The version identifies the ShellCheck directive contract represented by the YAML
mapping. Actionlint maintains this schema. Users can select their installed
ShellCheck version independently.
Runtime configuration parsing does not download schemas. Tests check relative
resolution for local packages, CDN releases and Git revisions with external
loading disabled.

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
