# commitish resolution provider

This executable supplies the optional actionlint dependency resolver protocol.
The `commitish` and `commitish-github` crates own ref selection and GitHub evidence
acquisition. Action and reusable-workflow profiles are passed separately.

```sh
cargo build --release --locked --manifest-path plugins/commitish/Cargo.toml
actionlint check --resolve-with plugins/commitish/target/release/actionlint-resolver-commitish
```

The provider is anonymous by default. To supply a token explicitly, add
`--resolver-arg=--token-env --resolver-arg=GITHUB_TOKEN`. Tokens never travel in
JSON requests, command arguments, results or ordinary error output.

The host sends one JSON request on stdin and reads one JSON response on stdout.
Requests contain protocol `version: 1`, `repository`, `ref`, `kind` (`action` or
`workflow`), and an optional `expected_repository_id`. Results retain the repository
ID, observation time, immutable commit or tree, and the complete commitish evidence.
Unknown or inaccessible refs fail resolution; they are not treated as nonexistent.
The provider performs read-only requests. It does not fetch or execute action code.

The host bounds responses to 2 MiB and each process to 30 seconds by default,
cancels the process with analysis, and caches exact requests within the provider
instance. Each analysis request creates a fresh cache, including LSP buffer invalidations.
Review pin suggestions with `actionlint fix --include-suggested --resolve-with ...`.
