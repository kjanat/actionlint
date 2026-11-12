# Buildkite runner provider

This Rust component maps `runs-on` labels to queues in a supplied snapshot. It
supplies operating-system, architecture, shell and path-case facts for checks.
It does not add Buildkite pipeline syntax, execute workflows or call the Agent API.

All requested labels and the optional group must match the same queue. Missing,
stale, incomplete or differently scoped snapshots produce unknown results rather
than claims that a runner does not exist. Runtime expressions also produce unknown
results. A malformed snapshot fails the request.

From the repository root, test and build the component:

```sh
mise exec -- cargo test --locked -p actionlint-example-buildkite-runners
mise exec -- cargo build --locked -p actionlint-example-buildkite-runners --target wasm32-wasip2 --profile wasm --lib
```

Package it using the public Rust SDK:

```sh
mise exec -- cargo run --locked -p actionlint-example-buildkite-runners --features package --bin package-buildkite-runners -- \
  target/wasm32-wasip2/wasm/buildkite_runners.wasm plugins/examples/buildkite-runners/dist
mise exec -- wasm-tools validate plugins/examples/buildkite-runners/dist/plugin.wasm
```

The directory contains the component, discovery manifest, empty option schema,
license and checksums. The [packager](src/bin/package.rs) reads the provider's
`describe()` result. Its published package ID, `example/buildkite-runners`, is
separate from the provider name, `buildkite-runners`, used by `--runner-provider`.

Run it with the supplied [snapshot](runners.json) and [workflow](example.yml):

```sh
mise exec go -- go run ./cmd/actionlint check --no-config --no-tool shellcheck --no-tool ruff \
  --plugin plugins/examples/buildkite-runners/dist/plugin.wasm \
  --runner-target buildkite --runner-provider buildkite-runners \
  --runner-scope my-org/my-repo --runner-snapshot plugins/examples/buildkite-runners/runners.json \
  plugins/examples/buildkite-runners/example.yml
```

The snapshot resolves that job to Windows with a `pwsh` default. Adding `shell: sh`
to its step produces a `shell-name` finding because that shell is unsupported on
Windows. No PowerShell linter is included in this provider.

The snapshot scope must equal `--runner-scope`. Its `freshness` and `complete`
fields describe the supplied inventory; the provider does not refresh or age it.
Each answer identifies the SHA-256 of the queue data passed to the provider. Tests cover queue
matching, platforms, uncertainty, malformed data and answer order.
