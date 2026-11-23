# Source-annotations plugin

This Rust component reports each YAML comment on a script declaration, including scripts in called composite actions.

Test and build the component from the repository root:

```sh
mise exec -- cargo test --locked -p actionlint-example-source-annotations
mise exec -- cargo build --locked -p actionlint-example-source-annotations --target wasm32-wasip2 --profile wasm --lib
```

Package the component:

```sh
mise exec -- cargo run --locked -p actionlint-example-source-annotations --features package --bin package-source-annotations -- \
  target/wasm32-wasip2/wasm/source_annotations.wasm plugins/examples/source-annotations/dist
mise exec -- wasm-tools validate plugins/examples/source-annotations/dist/plugin.wasm
```

Run the plugin against the supplied workflow:

```sh
mise exec go -- go run ./cmd/actionlint check --no-config --no-tool shellcheck --no-tool ruff \
  --plugin plugins/examples/source-annotations/dist/plugin.wasm plugins/examples/source-annotations/example.yml
```

The [workflow](example.yml) produces a `leading` finding on the comment:

```yaml
on: push
permissions: {}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      # Explain the command.
      - run: echo hello
```
