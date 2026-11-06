# Pinned-actions plugin

This Rust component warns when a remote action or reusable workflow uses a tag,
branch or short commit hash. It accepts full 40-character hexadecimal hashes.
Local paths, Docker references and malformed references are left to other checks.

The plugin reads dependency facts supplied by the host. It does not parse YAML,
resolve tags or access the network. Findings identify the dependency's captured
source revision and range. It cannot offer a replacement hash without resolving
the reference, so it reports no automatic fix.

From the repository root, run its tests and build the component:

```sh
mise exec -- cargo test --locked -p actionlint-example-pinned-actions
mise exec -- cargo build --locked -p actionlint-example-pinned-actions --target wasm32-wasip2 --profile wasm --lib
```

Package it using the public Rust SDK:

```sh
mise exec -- cargo run --locked -p actionlint-example-pinned-actions --features package --bin package-pinned-actions -- \
  target/wasm32-wasip2/wasm/pinned_actions.wasm plugins/examples/pinned-actions/dist
mise exec -- wasm-tools validate plugins/examples/pinned-actions/dist/plugin.wasm
```

The release directory contains the component, discovery metadata, option schema,
license and checksums. The [packaging entry point](src/bin/package.rs) reads
`Plugin::describe()` so packaging and execution use the same declaration.

Run the plugin against a workflow:

```sh
mise exec go -- go run ./cmd/actionlint check --no-config --no-tool shellcheck --no-tool ruff \
  --plugin plugins/examples/pinned-actions/dist/plugin.wasm workflow.yml
```

Replace `workflow.yml` with the workflow to check. Tests cover tags, branches,
short hashes, full hashes, unsupported dependency kinds and source locations.
