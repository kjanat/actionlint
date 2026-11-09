use std::{env, fs, path::PathBuf};

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let mut wit = PathBuf::from(env::var("DEP_ACTIONLINT_EXTENSION_API_WIT")?);
    if env::var_os("CARGO_FEATURE_V03").is_some() {
        wit = wit.join("deps/actionlint-0.3.0");
    }
    let world = if env::var_os("CARGO_FEATURE_TEMPLATE").is_some() {
        "template-script-engine-plugin"
    } else {
        "script-engine-plugin"
    };
    let output = PathBuf::from(env::var("OUT_DIR")?).join("bindings.rs");
    fs::write(
        output,
        format!(
            "wit_bindgen::generate!({{ path: {:?}, world: {:?} }});",
            wit.to_string_lossy(),
            world
        ),
    )?;
    Ok(())
}
