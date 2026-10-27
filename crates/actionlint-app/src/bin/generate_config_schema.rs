use std::{error::Error, path::Path};

fn main() -> Result<(), Box<dyn Error>> {
    let path = Path::new(env!("CARGO_MANIFEST_DIR")).join("config.schema.json");
    let generated = actionlint_app::config::schema_json()?;
    let args: Vec<_> = std::env::args_os().skip(1).collect();
    if args.is_empty() {
        std::fs::write(&path, generated)?;
    } else if args.len() == 1 && args[0] == "--check" {
        if std::fs::read_to_string(&path)? != generated {
            return Err(format!(
                "{} is stale; regenerate configuration schema",
                path.display()
            )
            .into());
        }
    } else {
        return Err("usage: generate-config-schema [--check]".into());
    }
    Ok(())
}
