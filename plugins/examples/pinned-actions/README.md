# Pinned-actions plugin

This Rust component warns when a remote action or reusable workflow uses a tag, branch or short commit hash. Full 40-character hashes pass.

Test and build the component from the repository root:

```sh
mise exec -- cargo test --locked -p actionlint-example-pinned-actions
mise exec -- cargo build --locked -p actionlint-example-pinned-actions --target wasm32-wasip2 --profile wasm --lib
```

```sh
mise exec -- cargo run --locked -p actionlint-example-pinned-actions --features package --bin package-pinned-actions -- \
  target/wasm32-wasip2/wasm/pinned_actions.wasm plugins/examples/pinned-actions/dist
mise exec -- wasm-tools validate plugins/examples/pinned-actions/dist/plugin.wasm
```

Run the plugin against a workflow:

```sh
mise exec go -- go run ./cmd/actionlint check --no-config --no-tool shellcheck --no-tool ruff \
  --plugin plugins/examples/pinned-actions/dist/plugin.wasm workflow.yml
```
