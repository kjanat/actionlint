use std::error::Error;
use std::fs;

use sha2::{Digest, Sha256};

fn main() -> Result<(), Box<dyn Error>> {
    println!("cargo:rerun-if-changed=wit");
    println!(
        "cargo::metadata=wit={}",
        std::path::Path::new(&std::env::var("CARGO_MANIFEST_DIR")?)
            .join("wit")
            .display()
    );
    for (file, variable) in [
        ("wit/analyzer.wit", "ACTIONLINT_ANALYZER_WIT_SHA256"),
        ("wit/runner.wit", "ACTIONLINT_RUNNER_WIT_SHA256"),
        (
            "wit/script-engine.wit",
            "ACTIONLINT_SCRIPT_ENGINE_02_WIT_SHA256",
        ),
        (
            "wit/template-effects.wit",
            "ACTIONLINT_TEMPLATE_EFFECTS_02_WIT_SHA256",
        ),
        (
            "wit/deps/actionlint-0.3.0/script-engine.wit",
            "ACTIONLINT_SCRIPT_ENGINE_03_WIT_SHA256",
        ),
        (
            "wit/deps/actionlint-0.3.0/template-effects.wit",
            "ACTIONLINT_TEMPLATE_EFFECTS_03_WIT_SHA256",
        ),
    ] {
        let wit = fs::read_to_string(file)?.replace("\r\n", "\n");
        println!(
            "cargo:rustc-env={variable}={}",
            Sha256::digest(wit.as_bytes())
                .iter()
                .map(|byte| format!("{byte:02x}"))
                .collect::<String>()
        );
    }
    Ok(())
}
