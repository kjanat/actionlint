use std::error::Error;
use std::path::Path;

use actionlint_extension_api::Guest;
use actionlint_extension_api::package::{Capabilities, Engine, Identity, Package};
use source_annotations::Plugin;

fn main() -> Result<(), Box<dyn Error>> {
    let args: Vec<_> = std::env::args_os().skip(1).collect();
    if args.len() != 2 {
        return Err("usage: package-source-annotations component.wasm output-directory".into());
    }
    let descriptor = Plugin::describe();
    Package {
        identity: Identity {
            id: "example/source-annotations",
            name: "Source annotations",
            description: &descriptor.description,
            license: "MIT",
            repository: "https://github.com/kjanat/actionlint",
            version: env!("CARGO_PKG_VERSION"),
        },
        engine: Engine {
            name: "source-annotations",
            version: env!("CARGO_PKG_VERSION"),
        },
        languages: &[],
        capabilities: Capabilities {
            filesystem: false,
            network: false,
            fixes: false,
            source_annotations: true,
            source_files: false,
        },
        descriptor: &descriptor,
        license_text: include_str!("../../LICENSE"),
    }
    .write(Path::new(&args[0]), Path::new(&args[1]))
}
