# actionlint-extension-api

Rust analyzer bindings and native packaging for actionlint components.
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
            maturity: "nursery".into(),
            provider: "example".into(),
            recommended: false,
            explanation: "A starting point for an analyzer.".into(),
            help: "Implement analyze to report findings.".into(),
            documentation_url: String::new(),
            fix_availability: String::new(), // Legacy ABI field; ignored by the host.
            options_schema: "{}".into(),
            examples: Vec::new(),
            required_inputs: Vec::new(),
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

See [`pipe-to-shell.rs`](https://github.com/kjanat/actionlint/blob/master/plugins/examples/pipe-to-shell/src/lib.rs)
for an analyzer that reports findings.

Build extensions for `wasm32-wasip2`. The default analyzer API imports host logging.
Enable `source-annotations` to import source comments through the optional
`kjanat:actionlint/source-annotations@0.2.0` interface.
This requires a host implementing that additional interface.

Enable `tool-integration` to implement the `managed-plugin` world. Its optional
`tool-integration@0.2.0` export advertises a tool identity and versioned
configuration profile, then accepts or rejects the host configuration. The
analyzer descriptor name must equal that tool identity. Distribution names,
repository names and engine names do not participate in replacement selection.
Negotiation receives host profile configuration and explicit caller plugin
options separately. The guest combines them and validates the effective options.
Accepted options are opaque to the host; filesystem grants are separate typed
requests. The [shell configuration profile](json/shellcheck-config-0.1.0.md)
and its [JSON Schema](json/shellcheck-config-0.1.0.json) are part of the public
contract and ship in this package.

That world imports `source-files@0.2.0` for explicit source/WASI mount mappings
and `kjanat:actionlint-script/shell-script-inputs@0.3.0` for host-prepared shell text, masking, source lines
and exclusions. Preparation is restricted to script references supplied in the
current analysis. Unknown references fail; unsupported shell scripts return
`none`. Guests must not infer mounts from host path conventions, prepare a
second copy of the same script, or infer behavior from diagnostic wording.

Enable `script-environments` to query scoped environment names without their
values. Call `kjanat::actionlint_script::environments::for_script` with an
occurrence supplied in the current analysis. Each OS alternative has its own
runner merge comparison, shell lookup comparison and names. An unresolved name
is not proof that a variable is missing. Snippet-engine components can import
the same interface and call `current` during their check.

```rust
#[cfg(feature = "script-environments")]
fn environments_for(
    script: &actionlint_extension_api::Script,
) -> Result<
    Vec<actionlint_extension_api::kjanat::actionlint_script::environments::Environment>,
    String,
> {
    let occurrence = script
        .occurrence
        .as_deref()
        .ok_or("missing script occurrence")?;
    actionlint_extension_api::kjanat::actionlint_script::environments::for_script(occurrence)
}
```

Script engines implement the `script-engine-plugin` world of
[`wit/script-engine.wit`](wit/script-engine.wit) (0.2.0) or
[`wit/deps/actionlint-0.3.0/script-engine.wit`](wit/deps/actionlint-0.3.0/script-engine.wit) (0.3.0, adding fixes,
reported sources, excluded codes and the directory resource). Each version is its
own WIT package directory; generate bindings from the directory of the version
the engine exports. This crate's `bindings` module covers the analyzer worlds only.

Enable `package` for a native packaging binary. Construct `package::Package`
from the extension's own descriptor, Cargo identity, engine, languages,
capabilities and license text, then call `write(input, output)`. It writes
`plugin.wasm`, `metadata.json`, `schema.json`, `LICENSE` and `SHA256SUMS`.
Only the outer discovery section is replaced; nested components/modules survive.

For a `runner-provider@0.2.0` component, use `package::RunnerPackage`. Generate
guest bindings from `wit/runner.wit` with world `runner-plugin`; the analyzer
`Guest` trait does not implement that role. Supply the provider name, version and
targets from the runner's `describe()` result. Package identity, display name and
release version are separate from those provider fields. The
[Buildkite example](https://github.com/kjanat/actionlint/tree/master/plugins/examples/buildkite-runners)
contains native tests, a packaging entry point, snapshot data and a workflow command.

Set `package::Identity.id` to the stable published package identifier. This is
the registry and lockfile identity; it can differ from the analyzer's rule name
and the package's display name. Keep it unchanged across releases. Tool
integration identifies its tool through the analyzer's rule name.

The protocol source is [`plugins/protocol/wit/`](https://github.com/kjanat/actionlint/tree/master/plugins/protocol/wit). Host generation, examples and
fixtures read these same files. Cargo includes them in the published package.

For local SDK development, see [CONTRIBUTING.md](https://github.com/kjanat/actionlint/blob/master/plugins/sdk/rust/CONTRIBUTING.md).
