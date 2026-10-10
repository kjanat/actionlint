# Ruff selector metadata

The configuration parser and editor schema share the checked-in selector list in
`internal/ruff/selectors_generated.go`. Configuration loading is offline.

When updating the bundled Ruff release:

1. Update `release` and its immutable upstream `revision` in `main.go` alongside
   the Action's Ruff asset manifest.
2. Run `go run ./scripts/generate-ruff-selectors` from the repository root. This
   explicit update downloads the pinned Ruff schema and selector redirects.
3. Run `go generate -run generate-config-schema` and `dprint fmt`.
4. Run the Ruff configuration, schema, and selector generator tests.

The upstream schema includes rule codes and prefixes, including preview rule
codes. Human-readable names and categories require Ruff preview mode, which this
adapter does not enable. Compatibility redirects whose targets remain in the
schema are included. Removed and test-only selectors are excluded.
