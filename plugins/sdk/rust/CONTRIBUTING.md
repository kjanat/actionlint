# Contributing to the SDK

## Local development

Point the plugin's manifest at an actionlint checkout:

```toml
[package]
name    = "my-actionlint-extension"
version = "0.1.0"
edition = "2024"

[lib]
crate-type = ["cdylib"]

[dependencies]
actionlint-extension-api = { path = "../actionlint/plugins/sdk/rust" }
```

`run plugins-check-rust-sdk` builds the script-engine consumer and loads it through the Go host.

## Publishing to crates.io

Run [Publish Rust SDK](../../../.github/workflows/sdk-publish.yml) from `master` with the version in [Cargo.toml](Cargo.toml). Dry-run is on by default.

```sh
gh workflow run sdk-publish.yml --ref master -f version=0.2.0 -F dry-run=true
```

Verify locally with `run plugins-check-native`, then:

```sh
cargo fmt -p actionlint-extension-api --check
cargo clippy --locked -p actionlint-extension-api --all-targets --all-features -- -D warnings
cargo publish --locked -p actionlint-extension-api --all-features --registry crates-io --dry-run
```

The [first publication needs an API token](https://crates.io/docs/trusted-publishing). Run it with `authentication=bootstrap-token` and dry-run off:

```sh
gh workflow run sdk-publish.yml --ref master -f version=0.2.0 -F dry-run=false -f authentication=bootstrap-token
```

Then add this trusted publisher on crates.io:

| Setting           | Value             |
| ----------------- | ----------------- |
| Repository owner  | `kjanat`          |
| Repository        | `actionlint`      |
| Workflow filename | `sdk-publish.yml` |
| Environment       | `crates-io`       |

Use `authentication=trusted-publishing` for later versions, and remove the bootstrap secret after trusted publishing succeeds.
