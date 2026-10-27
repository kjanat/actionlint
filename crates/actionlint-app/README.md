# Application configuration

CLI and LSP load the same JSON configuration with `--config FILE`. Component
paths resolve relative to that file. Repeated `--plugin` options add components;
duplicate paths are rejected. Unknown configuration fields are errors.

```json
{
  "$schema": "config.schema.json",
  "plugins": ["plugins/workflow-rules.wasm"]
}
```

`config::Config` owns both deserialization and the generated JSON Schema.
Generate after changing configuration fields:

```sh
cargo run -p actionlint-app --features schema --bin generate-config-schema
```

Check the committed artifact without rewriting it:

```sh
cargo run -p actionlint-app --features schema --bin generate-config-schema -- --check
```

The `schema` feature also enables a test comparing the artifact with Schemars
output. Schema generation dependencies are absent from normal application builds.
