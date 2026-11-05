use std::error::Error;
use std::fs;

use sha2::{Digest, Sha256};

fn main() -> Result<(), Box<dyn Error>> {
    println!("cargo:rerun-if-changed=wit");
    let wit = fs::read_to_string("wit/analyzer.wit")?.replace("\r\n", "\n");
    println!(
        "cargo:rustc-env=ACTIONLINT_ANALYZER_CONTRACT_SHA256={:x}",
        Sha256::digest(wit.as_bytes())
    );
    Ok(())
}
