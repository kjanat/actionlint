# actionlint-extension-api

This crate provides Rust bindings and native packaging for actionlint components. Implement `actionlint_extension_api::Guest`, export it with `actionlint_extension_api::export_plugin!(Plugin)` on `wasm32`, and build for `wasm32-wasip2`.

A minimal analyzer:

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

[`pipe-to-shell.rs`](https://github.com/kjanat/actionlint/blob/master/plugins/examples/pipe-to-shell/src/lib.rs) is an analyzer that reports findings.

## Features

- `reference-resolver` implements the `reference-resolver-plugin` world in the `reference_resolver` module.
- `source-annotations` imports source comments through `kjanat:actionlint/source-annotations@0.2.0`.
- `shell-script-inputs` adds `kjanat::actionlint_script::shell_script_inputs::for_script`. It returns masked text, startup commands and a line map for an occurrence in the current analysis.
- `script-environments` adds `kjanat::actionlint_script::environments::for_script`, which returns scoped environment names without values. An unresolved name may still exist at runtime.
- `tool-integration` implements the `managed-plugin` world.
- `package` adds the `package` module described under [Packaging](#packaging).

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

## Tool integration

The `tool-integration@0.2.0` export advertises a tool identity and configuration profile. The analyzer descriptor name must equal that tool identity. The guest receives host configuration and plugin options separately, combines them and validates the result.

Use the shell-script-inputs line map for locations. `shell_script_inputs::for_script` fails on an unknown script ID and returns `None` for an invalid interpreter invocation.

## Script engines

Implement the script-engine and template-effects guest traits from `script_engine::v0_2` or `script_engine::v0_3`, then export them:

```rust
#[cfg(target_arch = "wasm32")]
actionlint_extension_api::script_engine::v0_2::export_template_script_engine_v0_2!(
    Plugin with_types_in actionlint_extension_api::script_engine::v0_2
);
```

For 0.3.0, use `v0_3::export_template_script_engine_v0_3!` and `with_types_in actionlint_extension_api::script_engine::v0_3`. The `with_types_in` argument is required. The [small consumer](https://github.com/kjanat/actionlint/blob/master/plugins/sdk/rust/tests/script-engine-consumer/src/lib.rs) implements both versions.

To generate the `script-engine-plugin` or `template-script-engine-plugin` world yourself, add the SDK as a direct dependency and read its WIT path in `build.rs`:

```rust,no_run
use std::{env, fs, path::PathBuf};

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let wit = PathBuf::from(env::var("DEP_ACTIONLINT_EXTENSION_API_WIT")?);
    let output = PathBuf::from(env::var("OUT_DIR")?).join("bindings.rs");
    fs::write(
        output,
        format!(
            "wit_bindgen::generate!({{ path: {:?}, world: \"script-engine-plugin\" }});",
            wit.to_string_lossy()
        ),
    )?;
    Ok(())
}
```

Include the result with `include!(concat!(env!("OUT_DIR"), "/bindings.rs"));` and depend on `wit-bindgen` in the guest. For 0.3.0, append `deps/actionlint-0.3.0` to `wit` first. Package the engine with `package::ScriptPackage`, the matching `script_engine::v0_2` or `script_engine::v0_3` descriptor type and a `ScriptWorld` that matches the exported world. Build the package descriptor from the same values as the guest descriptor.

## Packaging

Construct `package::Package` from the descriptor, Cargo identity, engine, languages, capabilities and license text. `write(input, output)` writes `plugin.wasm`, `metadata.json`, `schema.json`, `LICENSE` and `SHA256SUMS`.

`package::Identity.id` is the registry and lockfile identity. Keep it unchanged across releases. It may differ from the rule and display names.

## Runner providers

Use `package::RunnerPackage` with `interface_version` set to `package::RunnerVersion::V02` or `V03`. Generate bindings for world `runner-plugin` from `wit/runner.wit` for 0.2.0 or from `wit/deps/actionlint-0.3.0` for 0.3.0. Take the provider name, version and targets from the runner's `describe()` result. See the [Buildkite example](https://github.com/kjanat/actionlint/tree/master/plugins/examples/buildkite-runners).

For local SDK development, see [CONTRIBUTING.md](https://github.com/kjanat/actionlint/blob/master/plugins/sdk/rust/CONTRIBUTING.md).
