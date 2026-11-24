# Pipe-to-shell plugin

This Rust component warns about commands such as `curl URL | bash` and `wget URL | sh`, including scripts inside called actions.

Test and build the component from the repository root:

```sh
mise exec -- cargo test --locked -p actionlint-example-pipe-to-shell --lib
mise exec -- cargo build --locked -p actionlint-example-pipe-to-shell --target wasm32-wasip2 --profile wasm --lib
```

Package the component:

```sh
mise exec -- cargo run --locked -p actionlint-example-pipe-to-shell --features package --bin package-pipe-to-shell -- \
  target/wasm32-wasip2/wasm/pipe_to_shell.wasm plugins/examples/pipe-to-shell/dist
mise exec -- wasm-tools validate plugins/examples/pipe-to-shell/dist/plugin.wasm
```

Try the component against a workflow:

```sh
mise exec go -- go run ./cmd/actionlint check --no-config --no-tool shellcheck --no-tool ruff \
  --plugin plugins/examples/pipe-to-shell/dist/plugin.wasm workflow.yml
```
