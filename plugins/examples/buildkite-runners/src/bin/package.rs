use std::error::Error;
use std::path::Path;

use actionlint_extension_api::package::{Identity, RunnerPackage};
use buildkite_runners::Plugin;
use buildkite_runners::exports::kjanat::actionlint0_2_0::runner_provider::Guest;

fn main() -> Result<(), Box<dyn Error>> {
    let args: Vec<_> = std::env::args_os().skip(1).collect();
    if args.len() != 2 {
        return Err("usage: package-buildkite-runners component.wasm output-directory".into());
    }
    let descriptor = Plugin::describe();
    RunnerPackage {
        identity: Identity {
            id: "example/buildkite-runners",
            name: "Buildkite queue runners",
            description: "Resolve runner labels from a supplied Buildkite queue snapshot",
            license: "MIT",
            repository: "https://github.com/kjanat/actionlint",
            version: env!("CARGO_PKG_VERSION"),
        },
        provider_name: &descriptor.name,
        provider_version: &descriptor.version,
        targets: &descriptor.targets,
        license_text: include_str!("../../LICENSE"),
    }
    .write(Path::new(&args[0]), Path::new(&args[1]))
}
