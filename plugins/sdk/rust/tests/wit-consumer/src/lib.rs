include!(concat!(env!("OUT_DIR"), "/bindings.rs"));
#[cfg(feature = "v03")]
use exports::kjanat::actionlint::script_engine::*;
#[cfg(not(feature = "v03"))]
use exports::kjanat::actionlint0_2_0::script_engine::*;
include!("common.rs");

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
            diagnostics: vec![],
            error: None,
        }
    }
}
#[cfg(all(feature = "template", feature = "v03"))]
use exports::kjanat::actionlint::template_effects as effects;
#[cfg(all(feature = "template", not(feature = "v03")))]
use exports::kjanat::actionlint0_2_0::template_effects as effects;
#[cfg(feature = "template")]
impl effects::Guest for Plugin {
    fn contract() -> String {
        "kjanat:actionlint/template-effects@0.1.0".into()
    }
    fn check(_: effects::Request) -> effects::Outcome {
        effects::Outcome {
            diagnostics: vec![],
            error: None,
        }
    }
}
export!(Plugin);
