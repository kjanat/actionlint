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
        Outcome {
            diagnostics: Vec::new(),
            error: None,
        }
    }
}

#[cfg(target_arch = "wasm32")]
actionlint_extension_api::export_plugin!(Plugin);
```

See [`pipe-to-shell.rs`](https://github.com/kjanat/actionlint/blob/master/examples/plugins/pipe-to-shell.rs)
for an analyzer that reports findings.

Build extensions for `wasm32-wasip2`. The default API imports only the original
host logging interface. Enable `source-annotations` to import source comments
through the optional `kjanat:actionlint/source-annotations@0.1.0` interface.
This requires a host implementing that additional interface.

Enable `tool-integration` to implement the `managed-plugin` world. Its optional
`tool-integration@0.1.0` export advertises a tool identity and versioned
configuration profile, then accepts or rejects the host configuration. The
analyzer descriptor name must equal that tool identity. Distribution names,
repository names and engine names do not participate in replacement selection.
Negotiation receives host profile configuration and explicit caller plugin
options separately. The guest combines them and validates the effective options.
Accepted options are opaque to the host; filesystem grants are separate typed
requests. The [shell configuration profile](schemas/shellcheck-config-0.1.0.md)
and its [JSON Schema](schemas/shellcheck-config-0.1.0.json) are part of the public
contract and ship in this package.

That world imports `source-files@0.1.0` for explicit source/WASI mount mappings
and `prepared-scripts@0.1.0` for host-prepared shell text, masking, source lines
and exclusions. Preparation is restricted to script references supplied in the
current analysis. Unknown references fail; unsupported shell scripts return
`none`. Guests must not infer mounts from host path conventions, prepare a
second copy of the same script, or infer behavior from diagnostic wording.

Enable `package` for a native packaging binary. Construct `package::Package`
from the extension's own descriptor, Cargo identity, engine, languages,
capabilities and license text, then call `write(input, output)`. It writes
`plugin.wasm`, `metadata.json`, `schema.json`, `LICENSE` and `SHA256SUMS`.
Only the outer discovery section is replaced; nested components/modules survive.

Set `package::Identity.id` to the stable published package identifier. This is
the registry and lockfile identity; it can differ from the analyzer's rule name
and the package's display name. Keep it unchanged across releases. Tool
integration identifies its tool through the analyzer's rule name.

This crate owns the public contracts in [`wit/`](https://github.com/kjanat/actionlint/tree/master/crates/actionlint-extension-api/wit). Host generation, examples and
fixtures read these same files. Cargo includes them in the published package.

For local SDK development, see [CONTRIBUTING.md](https://github.com/kjanat/actionlint/blob/master/crates/actionlint-extension-api/CONTRIBUTING.md).
