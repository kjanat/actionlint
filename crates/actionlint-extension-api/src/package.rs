//! Native component packaging. Plugin projects supply their own identity and data.

use std::borrow::Cow;
use std::error::Error;
use std::fs;
use std::path::Path;

use serde::Serialize;
use serde_json::json;
use sha2::{Digest, Sha256};
use wasm_encoder::{Component, CustomSection, RawSection};
use wasmparser::{Encoding, Parser, Payload, Validator};

use crate::{ANALYZER_CONTRACT_SHA256, ANALYZER_INTERFACE, Descriptor, Fact};

/// Discovery section read by actionlint without starting a guest.
pub const METADATA_SECTION: &str = "actionlint.plugin";

type Result<T> = std::result::Result<T, Box<dyn Error>>;

/// Package identity supplied by the extension's Cargo manifest.
pub struct Identity<'a> {
    pub name: &'a str,
    pub description: &'a str,
    pub license: &'a str,
    pub repository: &'a str,
    pub version: &'a str,
}

/// Analysis engine embedded by the extension.
#[derive(Serialize)]
pub struct Engine<'a> {
    pub name: &'a str,
    pub version: &'a str,
}

/// Resources and features the extension uses.
#[derive(Serialize)]
pub struct Capabilities {
    pub filesystem: bool,
    pub network: bool,
    pub fixes: bool,
    pub source_annotations: bool,
}

/// Extension-owned declarations and license for a component distribution.
pub struct Package<'a> {
    pub identity: Identity<'a>,
    pub engine: Engine<'a>,
    pub languages: &'a [&'a str],
    pub capabilities: Capabilities,
    pub descriptor: &'a Descriptor,
    pub license_text: &'a str,
}

fn digest(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}

fn pretty_json(value: &serde_json::Value) -> Result<Vec<u8>> {
    let mut bytes = serde_json::to_vec_pretty(value)?;
    bytes.push(b'\n');
    Ok(bytes)
}

impl Package<'_> {
    /// Serialize discovery metadata from the same descriptor the guest exports.
    pub fn metadata(&self) -> Result<Vec<u8>> {
        let rule = self.descriptor;
        let schema: serde_json::Value = serde_json::from_str(&rule.options_schema)?;
        let requires: Vec<_> = rule
            .requires
            .iter()
            .map(|fact| match fact {
                Fact::Source => "source",
                Fact::Scripts => "scripts",
                Fact::Mappings => "mappings",
                Fact::Invocations => "invocations",
                Fact::Execution => "execution",
                Fact::Dependencies => "dependencies",
            })
            .collect();
        let examples: Vec<_> = rule
            .examples
            .iter()
            .map(|example| {
                json!({
                    "description": example.description,
                    "before": example.before,
                    "after": example.after,
                })
            })
            .collect();
        let mut descriptor = json!({
            "name": rule.name,
            "description": rule.description,
            "category": rule.category,
            "subcategory": rule.subcategory,
            "maturity": rule.maturity,
            "provider": rule.provider,
            "recommended": rule.recommended,
            "explanation": rule.explanation,
            "help": rule.help,
            "documentation_url": rule.documentation_url,
            "fix_availability": rule.fix_availability,
            "options_schema": schema,
            "requires": requires,
        });
        if !examples.is_empty() {
            descriptor["examples"] = json!(examples);
        }
        pretty_json(&json!({
            "metadata_version": 1,
            "id": rule.name,
            "name": self.identity.name,
            "description": self.identity.description,
            "license": self.identity.license,
            "repository": self.identity.repository,
            "plugin_version": self.identity.version,
            "transport": "wasm-component",
            "interface": ANALYZER_INTERFACE,
            "contract_sha256": ANALYZER_CONTRACT_SHA256,
            "engine": self.engine,
            "languages": self.languages,
            "capabilities": self.capabilities,
            "descriptor": descriptor,
        }))
    }

    /// Write the component, discovery/schema sidecars, license and SHA256SUMS.
    pub fn write(&self, input: &Path, output: &Path) -> Result<()> {
        let metadata = self.metadata()?;
        let wasm = with_metadata(&fs::read(input)?, &metadata)?;
        let schema = pretty_json(&serde_json::from_str(&self.descriptor.options_schema)?)?;
        let files = [
            ("plugin.wasm", wasm),
            ("metadata.json", metadata),
            ("schema.json", schema),
            (
                "LICENSE",
                self.license_text.replace("\r\n", "\n").into_bytes(),
            ),
        ];
        let checksums = files
            .iter()
            .map(|(name, bytes)| format!("{}  {name}\n", digest(bytes)))
            .collect::<String>();
        fs::create_dir_all(output)?;
        for (name, bytes) in files {
            fs::write(output.join(name), bytes)?;
        }
        fs::write(output.join("SHA256SUMS"), checksums)?;
        Ok(())
    }
}

/// Replace only the outer component's discovery section, preserving nested modules.
pub fn with_metadata(input: &[u8], metadata: &[u8]) -> Result<Vec<u8>> {
    Validator::new().validate_all(input)?;
    let mut component = Component::new();
    component.section(&CustomSection {
        name: Cow::Borrowed(METADATA_SECTION),
        data: Cow::Borrowed(metadata),
    });
    let mut depth = 0;
    for payload in Parser::new(0).parse_all(input) {
        let payload = payload?;
        if let Payload::Version { encoding, .. } = payload {
            if depth == 0 && encoding != Encoding::Component {
                return Err("input must be a WebAssembly component".into());
            }
            depth += 1;
        } else if matches!(payload, Payload::End(_)) {
            depth -= 1;
        } else if depth == 1 {
            if let Payload::CustomSection(section) = &payload
                && section.name() == METADATA_SECTION
            {
                continue;
            }
            if let Some((id, range)) = payload.as_section() {
                let range = usize::try_from(range.start)?..usize::try_from(range.end)?;
                component.section(&RawSection {
                    id,
                    data: &input[range],
                });
            }
        }
    }
    Ok(component.finish())
}

#[cfg(test)]
mod tests {
    use super::*;
    use wasm_encoder::{Module, ModuleSection};

    // Repackaging preserves unrelated outer sections and nested core metadata.
    #[test]
    fn packaging_is_idempotent_and_keeps_nested_custom_sections() -> Result<()> {
        let mut module = Module::new();
        module.section(&CustomSection {
            name: Cow::Borrowed(METADATA_SECTION),
            data: Cow::Borrowed(b"nested"),
        });
        let mut component = Component::new();
        component.section(&CustomSection {
            name: Cow::Borrowed("other"),
            data: Cow::Borrowed(b"preserved"),
        });
        component.section(&ModuleSection(&module));
        let metadata = b"{}";
        let packaged = with_metadata(&component.finish(), metadata)?;
        assert_eq!(packaged, with_metadata(&packaged, metadata)?);
        Validator::new().validate_all(&packaged)?;
        let mut sections = Vec::new();
        for payload in Parser::new(0).parse_all(&packaged) {
            if let Payload::CustomSection(section) = payload? {
                sections.push((section.name().to_string(), section.data().to_vec()));
            }
        }
        assert_eq!(
            sections,
            vec![
                (METADATA_SECTION.to_string(), metadata.to_vec()),
                ("other".to_string(), b"preserved".to_vec()),
                (METADATA_SECTION.to_string(), b"nested".to_vec()),
            ]
        );
        Ok(())
    }

    // Reject a valid core module and a component with a truncated section payload.
    #[test]
    fn packaging_rejects_core_modules_and_truncated_components() {
        assert!(with_metadata(&Module::new().finish(), b"{}").is_err());
        assert!(with_metadata(&[0, 97, 115, 109, 13, 0, 1, 0, 1, 127], b"{}").is_err());
    }
}
