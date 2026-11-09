use std::{env, error::Error, path::Path};
use actionlint_extension_api::package::{Identity, ScriptPackage, ScriptWorld};
#[cfg(not(feature = "v03"))]
use actionlint_extension_api::script_engine::v0_2::exports::kjanat::actionlint0_2_0::script_engine::*;
#[cfg(feature = "v03")]
use actionlint_extension_api::script_engine::v0_3::exports::kjanat::actionlint::script_engine::*;
include!("../common.rs");

fn main() -> Result<(), Box<dyn Error>> {
    let args: Vec<_> = env::args().collect();
    let descriptor = descriptor();
    let package = ScriptPackage {
        identity: Identity {
            id: "example",
            name: "Example",
            description: "Example engine",
            license: "MIT",
            repository: "https://example.com",
            version: "1.0.0",
        },
        descriptor: &descriptor,
        world: if cfg!(feature = "template") {
            ScriptWorld::TemplateScriptEngine
        } else {
            ScriptWorld::ScriptEngine
        },
        script_inputs: &[],
        license_text: "MIT",
    };
    package.write(Path::new(&args[1]), Path::new(&args[2]))
}
