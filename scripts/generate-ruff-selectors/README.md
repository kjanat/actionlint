# Ruff selector metadata

The editor schema references the unchanged upstream Ruff schema in
`schemas/ruff/0.17.0.schema.json`. The adjacent provenance file records its release,
immutable source revision, public release URL, and SHA-256 digest. Do not format
or edit the upstream snapshot. The release's upstream license notice is bundled
beside it as `0.17.0.LICENSE.txt`.

The generated `0.17.0-selectors.schema.json` wrapper references the upstream
`RuleSelector` definition and records just the compatibility differences described
below. It does not duplicate the supported selector catalogue. Python target
versions reference the upstream `PythonVersion` definition directly.

The configuration parser uses generated selector and Python target tables in
`internal/ruff/selectors_generated.go`, derived from the same upstream metadata.
Configuration loading is offline; it does not download or interpret schemas.

When updating the bundled Ruff release:

1. Update `release` and its immutable upstream `revision` in `main.go` alongside
   the Action's Ruff asset manifest.
2. Install the pinned Ruff release, then run `go run ./scripts/generate-ruff-selectors`
   from the repository root (`-ruff /path/to/ruff` selects its executable). This
   explicit update verifies the Ruff version, reads its rule metadata, and downloads
   the pinned Ruff schema and selector redirects. It writes the byte-identical
   schema snapshot, license notice, provenance, compatibility wrapper, and runtime tables.
3. Run `go generate -run generate-config-schema` and `dprint fmt`.
4. Run the Ruff configuration, schema, and selector generator tests.

When changing the snapshot version, update the exact upstream file exclusion in
`.dprint.jsonc`. Keep previously published schema paths available.

Only codes and prefixes matching at least one non-preview rule are supported.
Human-readable names and categories require Ruff preview mode, which this
adapter does not enable. Compatibility redirects to supported selectors are
included. Preview-only, removed, and test-only selectors are excluded.
