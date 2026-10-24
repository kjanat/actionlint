# actionlint-extension-api

Rust guest bindings and native packaging for actionlint analyzer components.
Implement `actionlint_extension_api::Guest`, then export the implementation with
`actionlint_extension_api::export_plugin!(Plugin)` on `wasm32`.

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

This complete `src/lib.rs` defines an analyzer that reports no findings:

```rust
use actionlint_extension_api::{Descriptor, Guest, Input, Outcome};

pub struct Plugin;

impl Guest for Plugin {
    fn describe() -> Descriptor {
        Descriptor {
            name: "my-extension".into(),
            description: "Example analyzer".into(),
            category: "correctness".into(),
            subcategory: "scripts".into(),
            maturity: "experimental".into(),
            provider: "example".into(),
            recommended: false,
            explanation: "A starting point for an analyzer.".into(),
            help: "Implement analyze to report findings.".into(),
            documentation_url: String::new(),
            fix_availability: "none".into(),
            options_schema: "{}".into(),
            examples: Vec::new(),
            requires: Vec::new(),
        }
    }

    fn analyze(_input: Input) -> Outcome {
        Outcome { diagnostics: Vec::new(), error: None }
    }
}

#[cfg(target_arch = "wasm32")]
actionlint_extension_api::export_plugin!(Plugin);
```

Build extensions for `wasm32-wasip2`. The default API imports only the original
host logging interface. Enable `source-annotations` to import source comments
through the optional `kjanat:actionlint/source-annotations@0.1.0` interface.
This requires a host implementing that additional interface.

Enable `package` for a native packaging binary. Construct `package::Package`
from the extension's own descriptor, Cargo identity, engine, languages,
capabilities and license text, then call `write(input, output)`. It writes
`plugin.wasm`, `metadata.json`, `schema.json`, `LICENSE` and `SHA256SUMS`.
Only the outer discovery section is replaced; nested components/modules survive.

`plugins/wit` in the actionlint repository owns the contracts. This crate bundles
the same files so published packages build independently. Its build script rejects
stale copies when the canonical files are present in the source workspace.
