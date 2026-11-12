use std::error::Error;
use std::path::Path;

use actionlint_extension_api::Guest;
use actionlint_extension_api::package::{Capabilities, Engine, Identity, Package};
use pinned_actions::Plugin;

fn main() -> Result<(), Box<dyn Error>> {
    let args: Vec<_> = std::env::args_os().skip(1).collect();
    if args.len() != 2 {
        return Err("usage: package-pinned-actions component.wasm output-directory".into());
    }
    let descriptor = Plugin::describe();
    Package {
        identity: Identity {
            id: "example/pinned-actions",
            name: "Pinned actions",
            description: &descriptor.description,
            license: "MIT",
            repository: "https://github.com/kjanat/actionlint",
            version: env!("CARGO_PKG_VERSION"),
        },
        engine: Engine {
            name: "pinned-actions",
            version: env!("CARGO_PKG_VERSION"),
        },
        languages: &[],
        capabilities: Capabilities {
            filesystem: false,
            network: false,
            fixes: false,
            source_annotations: false,
            source_files: false,
        },
        descriptor: &descriptor,
        license_text: include_str!("../../LICENSE"),
    }
    .write(Path::new(&args[0]), Path::new(&args[1]))
}
