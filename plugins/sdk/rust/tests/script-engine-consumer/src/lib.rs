#[cfg(not(feature = "v03"))]
use actionlint_extension_api::script_engine::v0_2 as bindings;
#[cfg(feature = "v03")]
use actionlint_extension_api::script_engine::v0_3 as bindings;

#[cfg(feature = "v03")]
use bindings::exports::kjanat::actionlint::script_engine::*;
#[cfg(feature = "v03")]
use bindings::exports::kjanat::actionlint::template_effects as effects;
#[cfg(not(feature = "v03"))]
use bindings::exports::kjanat::actionlint0_2_0::script_engine::*;
#[cfg(not(feature = "v03"))]
use bindings::exports::kjanat::actionlint0_2_0::template_effects as effects;

include!("../../wit-consumer/src/common.rs");

struct Plugin;

impl Guest for Plugin {
    fn describe() -> Descriptor {
        descriptor()
    }

    fn validate_config(_: String) -> Result<(), String> {
        Ok(())
    }

    fn check(_: Request) -> Outcome {
        Outcome {
            diagnostics: Vec::new(),
            error: None,
        }
    }
}

impl effects::Guest for Plugin {
    fn contract() -> String {
        "kjanat:actionlint/template-effects@0.1.0".into()
    }

    fn check(_: effects::Request) -> effects::Outcome {
        effects::Outcome {
            diagnostics: Vec::new(),
            error: None,
        }
    }
}

#[cfg(all(target_arch = "wasm32", not(feature = "v03")))]
bindings::export_template_script_engine_v0_2!(Plugin with_types_in bindings);
#[cfg(all(target_arch = "wasm32", feature = "v03"))]
bindings::export_template_script_engine_v0_3!(Plugin with_types_in bindings);
