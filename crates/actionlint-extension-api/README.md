# actionlint-extension-api

Rust guest bindings and native packaging for actionlint analyzer components.
Implement `actionlint_extension_api::Guest`, then export the implementation with
`actionlint_extension_api::export_plugin!(Plugin)` on `wasm32`.

A minimal analyzer that reports no findings:

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

See [`pipe-to-shell.rs`](https://github.com/kjanat/actionlint/blob/master/examples/plugins/pipe-to-shell.rs)
for an analyzer that reports findings, and the
[plugin guide](https://github.com/kjanat/actionlint/blob/master/docs/plugins.md#local-sdk-development)
for local development setup.

Build extensions for `wasm32-wasip2`. The default API imports only the original
host logging interface. Enable `source-annotations` to import source comments
through the optional `kjanat:actionlint/source-annotations@0.1.0` interface.
This requires a host implementing that additional interface.

Enable `package` for a native packaging binary. Construct `package::Package`
from the extension's own descriptor, Cargo identity, engine, languages,
capabilities and license text, then call `write(input, output)`. It writes
`plugin.wasm`, `metadata.json`, `schema.json`, `LICENSE` and `SHA256SUMS`.
Only the outer discovery section is replaced; nested components/modules survive.

This crate owns the public contracts in [`wit/`](https://github.com/kjanat/actionlint/tree/master/crates/actionlint-extension-api/wit). Host generation, examples and
fixtures read these same files. Cargo includes them in the published package.
