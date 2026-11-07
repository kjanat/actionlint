# Source-annotations plugin

This Rust component demonstrates the optional YAML-comment interface. It reports
each comment attached to a supplied script declaration, including scripts in
called composite actions. Each finding identifies the captured source revision,
comment position and whether the comment precedes or follows the declaration.

This is an inspection example: a reported comment is not a workflow error.
Comments inside a `run: |` scalar are script text and are not returned by this
interface. The plugin neither reads files nor uses the network, and it offers no
fixes. The host supplies comments only for scripts included in the current call.

From the repository root, test and build it:

```sh
mise exec -- cargo test --locked -p actionlint-example-source-annotations
mise exec -- cargo build --locked -p actionlint-example-source-annotations --target wasm32-wasip2 --profile wasm --lib
```

Package the component through the public Rust SDK:

```sh
mise exec -- cargo run --locked -p actionlint-example-source-annotations --features package --bin package-source-annotations -- \
  target/wasm32-wasip2/wasm/source_annotations.wasm plugins/examples/source-annotations/dist
mise exec -- wasm-tools validate plugins/examples/source-annotations/dist/plugin.wasm
```

The output directory contains the component, discovery manifest, option schema,
license and checksums. The [packager](src/bin/package.rs) reads
`Plugin::describe()` so the manifest and running component use the same rule
declaration. Its capability declaration enables the source-annotations import.

Run it against a workflow:

```sh
mise exec go -- go run ./cmd/actionlint check --no-config --no-tool shellcheck --no-tool ruff \
  --plugin plugins/examples/source-annotations/dist/plugin.wasm plugins/examples/source-annotations/example.yml
```

The supplied [workflow](example.yml) produces a `leading` finding on the comment:

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

The native tests cover comment placement, source identity, called scripts and
empty inputs. The component host tests execute the packaged guest against the
real comment import. Tests of unauthorized references use a separate guest owned
by the host test suite.
