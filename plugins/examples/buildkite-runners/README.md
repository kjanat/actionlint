# Buildkite runner provider

This Rust component maps `runs-on` labels to queues in a snapshot. All labels and the optional group must match one queue. A missing, stale, incomplete or differently scoped snapshot gives an unknown result. A malformed snapshot fails the request.

From the repository root:

```sh
mise exec -- cargo test --locked -p actionlint-example-buildkite-runners
mise exec -- cargo build --locked -p actionlint-example-buildkite-runners --target wasm32-wasip2 --profile wasm --lib
```

Package the component:

```sh
mise exec -- cargo run --locked -p actionlint-example-buildkite-runners --features package --bin package-buildkite-runners -- \
  target/wasm32-wasip2/wasm/buildkite_runners.wasm plugins/examples/buildkite-runners/dist
mise exec -- wasm-tools validate plugins/examples/buildkite-runners/dist/plugin.wasm
```

Run the component with the [snapshot](runners.json) and [workflow](example.yml). The snapshot scope must equal `--runner-scope`.

```sh
mise exec go -- go run ./cmd/actionlint check --no-config --no-tool shellcheck --no-tool ruff \
  --plugin plugins/examples/buildkite-runners/dist/plugin.wasm \
  --runner-target buildkite --runner-provider buildkite-runners \
  --runner-scope my-org/my-repo --runner-snapshot plugins/examples/buildkite-runners/runners.json \
  plugins/examples/buildkite-runners/example.yml
```

The snapshot resolves that job to Windows with a `pwsh` default. Adding `shell: sh` to the step produces a `shell-name` finding.
