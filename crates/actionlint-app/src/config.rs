//! Configuration shared by command-line and editor analysis.

use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};

/// Analyzer configuration for actionlint.
#[derive(Debug, Default, Serialize, Deserialize)]
#[cfg_attr(feature = "schema", derive(schemars::JsonSchema))]
#[serde(deny_unknown_fields, rename_all = "camelCase")]
pub struct Config {
    /// JSON Schema location used by editors.
    #[serde(rename = "$schema", default, skip_serializing_if = "Option::is_none")]
    pub schema: Option<String>,
    /// Analyzer components. Relative paths resolve from this configuration file.
    #[serde(default)]
    pub plugins: Vec<PathBuf>,
}

impl Config {
    pub fn load(path: &Path) -> Result<Self, String> {
        let bytes = std::fs::read(path).map_err(|error| format!("{}: {error}", path.display()))?;
        let mut config: Self = serde_json::from_slice(&bytes)
            .map_err(|error| format!("{}: {error}", path.display()))?;
        let directory = path.parent().unwrap_or_else(|| Path::new("."));
        for plugin in &mut config.plugins {
            if plugin.is_relative() {
                *plugin = directory.join(&*plugin);
            }
        }
        Ok(config)
    }
}

#[cfg(feature = "schema")]
pub fn schema_json() -> Result<String, serde_json::Error> {
    let schema = schemars::schema_for!(Config);
    let value = serde_json::to_value(schema)?;
    let sorted = json_schema_sort::sorted_schema(value);
    Ok(serde_json::to_string_pretty(&sorted)? + "\n")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn configuration_rejects_typos_and_wrong_types() {
        for text in [
            r#"{"plugin":[]}"#,
            r#"{"plugins":"one.wasm"}"#,
            r#"{"plugins":[42]}"#,
        ] {
            assert!(serde_json::from_str::<Config>(text).is_err(), "{text}");
        }
        let config: Config = serde_json::from_str(r#"{"$schema":"config.schema.json"}"#).unwrap();
        assert!(config.plugins.is_empty());
    }

    #[cfg(feature = "schema")]
    #[test]
    fn checked_in_schema_matches_configuration_types() {
        let path = Path::new(env!("CARGO_MANIFEST_DIR")).join("config.schema.json");
        assert_eq!(
            std::fs::read_to_string(path).unwrap(),
            schema_json().unwrap()
        );
    }
}
