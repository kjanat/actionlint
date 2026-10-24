# Contributing to the SDK

## Local development

For development beside an actionlint checkout, use this plugin manifest:

```toml
[package]
name    = "my-actionlint-extension"
version = "0.1.0"
edition = "2024"

[lib]
crate-type = ["cdylib"]

[dependencies]
actionlint-extension-api = { path = "../actionlint/crates/actionlint-extension-api" }
```

The path points at the SDK in the adjacent checkout. Use the analyzer implementation from the [README](README.md), then build the plugin with `cargo build --target wasm32-wasip2` from its own directory.
