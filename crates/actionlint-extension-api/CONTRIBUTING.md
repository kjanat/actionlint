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

## Publishing to crates.io

Run [Publish Rust SDK](../../.github/workflows/sdk-publish.yml) from `master`, supplying the version in this crate's [Cargo.toml](Cargo.toml). Dry-run is enabled by default. The workflow tests default and all features, runs clippy, and builds the packaged crate before publication.

```sh
gh workflow run sdk-publish.yml --ref master -f version=0.1.0 -F dry-run=true
```

For local verification, run these commands from the repository root:

```sh
cargo fmt -p actionlint-extension-api --check
cargo test --locked -p actionlint-extension-api
cargo test --locked -p actionlint-extension-api --all-features
cargo clippy --locked -p actionlint-extension-api --all-targets --all-features -- -D warnings
cargo publish --locked -p actionlint-extension-api --all-features --registry crates-io --dry-run
```

The [first publication requires an API token](https://crates.io/docs/trusted-publishing). Select `authentication=bootstrap-token` and disable dry-run when ready to publish. This uses `CARGO_REGISTRY_TOKEN` from the repository's `crates-io` environment. No tag or actionlint release is created.

```sh
gh workflow run sdk-publish.yml --ref master -f version=0.1.0 -F dry-run=false -f authentication=bootstrap-token
```

After the first publication, add a GitHub trusted publisher in the crate's crates.io settings:

| Setting           | Value             |
| ----------------- | ----------------- |
| Repository owner  | `kjanat`          |
| Repository        | `actionlint`      |
| Workflow filename | `sdk-publish.yml` |
| Environment       | `crates-io`       |

Use `authentication=trusted-publishing` for subsequent versions. It obtains a short-lived token through GitHub OIDC; authentication failure never falls back to the bootstrap token. Remove the bootstrap secret after trusted publishing succeeds. The GitHub `crates-io` environment permits deployments only from `master`.
