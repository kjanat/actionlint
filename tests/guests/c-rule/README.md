# C component conformance

`self_dependency.c` checks whether a job names itself in `needs`. It consumes
the generated WIT types and emits a diagnostic at the dependency's source range.
It imports no Rust implementation or private model definitions.

The component-conformance task builds this guest and the Rust workflow rules,
validates their binaries, then executes both through the component host. It also
checks rejection of an incompatible component and compiles this unchanged C
implementation against an incompatible WIT signature; that compilation must fail.

Required tools: Rust from `rust-toolchain.toml`, `wit-bindgen` 0.63.0,
`wasm-tools` 1.262.0, and WASI SDK 34. Set `WASI_SDK_PATH` to the SDK installation.
`WIT_BINDGEN` and `WASM_TOOLS` can select executables outside `PATH`.

```sh
cargo run -p xtask -- component-conformance
```

Generated bindings and component artifacts go under `target/`. The incompatible
WIT copy is temporary and is removed after the check. Missing tools, artifacts,
or failed checks produce a failing exit status.
