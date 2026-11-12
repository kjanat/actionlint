# Pipe-to-shell plugin

This Rust component warns about commands such as `curl URL | bash` and
`wget URL | sh`. It consumes the host's extracted script facts, including scripts
inside called actions. It does not download or execute those scripts.

From the repository root, run its tests and build the component:

```sh
mise exec -- cargo test --locked -p actionlint-example-pipe-to-shell --lib
mise exec -- cargo build --locked -p actionlint-example-pipe-to-shell --target wasm32-wasip2 --profile wasm --lib
```

Package it for distribution with the Rust SDK:

```sh
mise exec -- cargo run --locked -p actionlint-example-pipe-to-shell --features package --bin package-pipe-to-shell -- \
  target/wasm32-wasip2/wasm/pipe_to_shell.wasm plugins/examples/pipe-to-shell/dist
mise exec -- wasm-tools validate plugins/examples/pipe-to-shell/dist/plugin.wasm
```

The release directory contains the component, discovery metadata, option schema,
license and checksums. The [packaging entry point](src/bin/package.rs) calls
`Plugin::describe()` to obtain the same declaration exported by the guest.
Packaging runs natively; the guest build does not enable that feature.

Try it against a workflow:

```sh
mise exec go -- go run ./cmd/actionlint check --no-config --no-tool shellcheck --no-tool ruff \
  --plugin plugins/examples/pipe-to-shell/dist/plugin.wasm workflow.yml
```

Replace `workflow.yml` with the workflow to check. The tests cover direct pipes,
command separators, logical operators and quoted text. The example recognizes
simple command lines; it does not implement a complete shell parser.
