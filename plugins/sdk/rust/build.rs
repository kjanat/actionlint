use std::error::Error;
use std::fs;

use sha2::{Digest, Sha256};

fn main() -> Result<(), Box<dyn Error>> {
    println!("cargo:rerun-if-changed=wit");
    for (file, variable) in [
        ("wit/analyzer.wit", "ACTIONLINT_ANALYZER_WIT_SHA256"),
        ("wit/runner.wit", "ACTIONLINT_RUNNER_WIT_SHA256"),
    ] {
        let wit = fs::read_to_string(file)?.replace("\r\n", "\n");
        println!(
            "cargo:rustc-env={variable}={:x}",
            Sha256::digest(wit.as_bytes())
        );
    }
    Ok(())
}
