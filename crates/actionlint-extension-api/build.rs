use std::error::Error;
use std::fs;
use std::path::Path;

use sha2::{Digest, Sha256};

fn main() -> Result<(), Box<dyn Error>> {
    for name in ["analyzer.wit", "source-annotations.wit"] {
        let bundled = Path::new("wit").join(name);
        println!("cargo:rerun-if-changed={}", bundled.display());
        let canonical = Path::new("../../plugins/wit").join(name);
        if canonical.is_file() {
            println!("cargo:rerun-if-changed={}", canonical.display());
            if fs::read_to_string(&bundled)?.replace("\r\n", "\n")
                != fs::read_to_string(&canonical)?.replace("\r\n", "\n")
            {
                return Err(
                    format!("refresh wit/{name} from the canonical plugins/wit/{name}").into(),
                );
            }
        }
    }
    let wit = fs::read_to_string("wit/analyzer.wit")?.replace("\r\n", "\n");
    println!(
        "cargo:rustc-env=ACTIONLINT_ANALYZER_CONTRACT_SHA256={:x}",
        Sha256::digest(wit.as_bytes())
    );
    Ok(())
}
