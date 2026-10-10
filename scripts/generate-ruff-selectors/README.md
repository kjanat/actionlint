# Ruff selector metadata

The configuration parser and editor schema share the checked-in selector list in
`internal/ruff/selectors_generated.go`. Configuration loading is offline.

When updating the bundled Ruff release:

1. Update `release` and its immutable upstream `revision` in `main.go` alongside
   the Action's Ruff asset manifest.
2. Install the pinned Ruff release, then run `go run ./scripts/generate-ruff-selectors`
   from the repository root (`-ruff /path/to/ruff` selects its executable). This
   explicit update verifies the Ruff version, reads its rule metadata, and downloads
   the pinned Ruff schema and selector redirects.
3. Run `go generate -run generate-config-schema` and `dprint fmt`.
4. Run the Ruff configuration, schema, and selector generator tests.

Only codes and prefixes matching at least one non-preview rule are supported.
Human-readable names and categories require Ruff preview mode, which this
adapter does not enable. Compatibility redirects to supported selectors are
included. Preview-only, removed, and test-only selectors are excluded.
