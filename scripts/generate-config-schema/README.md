# Configuration schemas

Run `go generate -run generate-config-schema` from the repository root to update
`actionlint.schema.json`. Actionlint owns tool selection, enablement, and the union
of an rc path and inline directives. The inline ShellCheck directives use a `$ref`
to `schemas/shellcheck/0.11.0.schema.json`, relative to the main schema's location.
The root and tool schemas omit `$id` so their retrieval location determines reference resolution.
An installed npm package uses its bundled schema; a versioned CDN URL or Git
commit URL uses the schema from that same release or revision.

The version identifies the ShellCheck directive contract represented by the YAML
mapping. Actionlint maintains this schema. Users can select their installed
ShellCheck version independently.
Runtime configuration parsing does not download schemas. Tests check relative
resolution for local packages, CDN releases and Git revisions with external
loading disabled.

## Ruff definitions

Ruff selectors and Python target versions reuse definitions from the unmodified
upstream `schemas/ruff/0.17.0.schema.json`. Its provenance sidecar identifies the
release URL, immutable source revision, and content digest. The generated selector
wrapper adds compatibility aliases and excludes unsupported preview/removed
selectors; it does not copy the positive selector catalogue. Target versions use
the upstream definition directly.

The upstream document retains its draft-07 dialect; the root and selector wrapper
use draft 2020-12. Validators must support references between these dialects.
Relative references keep the root, wrapper, and upstream snapshot together in a
checkout, npm package, or versioned CDN path. Tests disable external loading and
check every upstream selector and runtime alias against the configuration parser.

Selector validation is an array-level constraint so editors do not offer excluded
upstream values as item completions. Some clients collect suggestions from every
referenced branch without applying its restrictions. Selector completion therefore
offers the small `F` and `F821` examples. Every supported code and compatibility
alias remains valid. Python target completion
uses the full upstream definition. When Node.js and `yaml-language-server` are
installed, the Go tests also exercise the real language service offline.

Run `scripts/generate-ruff-selectors` with the pinned Ruff executable before
regenerating the root schema when upgrading Ruff; see that generator's README.

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
