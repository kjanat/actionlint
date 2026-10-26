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

/// Stable package identity and release metadata.
pub struct Identity<'a> {
    pub id: &'a str,
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
    pub source_files: bool,
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

fn options_schema(raw: &str) -> Result<Option<serde_json::Value>> {
    if raw.is_empty() {
        return Ok(None);
    }
    Ok(Some(serde_json::from_str(raw)?))
}

impl Package<'_> {
    /// Serialize discovery metadata from the same descriptor the guest exports.
    pub fn metadata(&self) -> Result<Vec<u8>> {
        let id = self.identity.id;
        if !(1..=128).contains(&id.len())
            || !id.bytes().all(|byte| {
                byte.is_ascii_alphanumeric() || matches!(byte, b'.' | b'_' | b'/' | b'-')
            })
        {
            return Err(
                "package id must contain 1 to 128 ASCII letters, digits, '.', '_', '/' or '-'"
                    .into(),
            );
        }
        let rule = self.descriptor;
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
            "requires": requires,
        });
        if let Some(schema) = options_schema(&rule.options_schema)? {
            descriptor["options_schema"] = schema;
        }
        if !examples.is_empty() {
            descriptor["examples"] = json!(examples);
        }
        pretty_json(&json!({
            "metadata_version": 1,
            "id": id,
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
        let schema = pretty_json(
            &options_schema(&self.descriptor.options_schema)?.unwrap_or_else(|| json!({})),
        )?;
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
    use std::path::PathBuf;
    use std::time::{SystemTime, UNIX_EPOCH};
    use wasm_encoder::{Module, ModuleSection};

    struct TestDirectory(PathBuf);

    impl TestDirectory {
        fn new() -> Result<Self> {
            let path = std::env::temp_dir().join(format!(
                "actionlint-extension-api-{}-{}",
                std::process::id(),
                SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()
            ));
            fs::create_dir(&path)?;
            Ok(Self(path))
        }
    }

    impl Drop for TestDirectory {
        fn drop(&mut self) {
            if let Err(error) = fs::remove_dir_all(&self.0) {
                eprintln!("cannot remove test directory {}: {error}", self.0.display());
            }
        }
    }

    fn descriptor(options_schema: &str) -> Descriptor {
        Descriptor {
            name: "example".into(),
            description: String::new(),
            category: "correctness".into(),
            subcategory: String::new(),
            maturity: "experimental".into(),
            provider: "example".into(),
            recommended: false,
            explanation: String::new(),
            help: String::new(),
            documentation_url: String::new(),
            fix_availability: "none".into(),
            options_schema: options_schema.into(),
            examples: Vec::new(),
            requires: Vec::new(),
        }
    }

    fn package(descriptor: &Descriptor) -> Package<'_> {
        Package {
            identity: Identity {
                id: "example-package",
                name: "Example",
                description: "",
                license: "MIT",
                repository: "https://example.com/extension",
                version: "0.1.0",
            },
            engine: Engine {
                name: "example",
                version: "0.1.0",
            },
            languages: &[],
            capabilities: Capabilities {
                filesystem: false,
                network: false,
                fixes: false,
                source_annotations: false,
                source_files: false,
            },
            descriptor,
            license_text: "Example license\n",
        }
    }

    #[test]
    fn package_identity_is_independent_of_rule_and_display_names() -> Result<()> {
        let descriptor = descriptor("");
        let metadata: serde_json::Value =
            serde_json::from_slice(&package(&descriptor).metadata()?)?;
        assert_eq!(metadata["id"], "example-package");
        assert_eq!(metadata["name"], "Example");
        assert_eq!(metadata["descriptor"]["name"], descriptor.name);
        assert_ne!(metadata["id"], metadata["descriptor"]["name"]);
        Ok(())
    }

    #[test]
    fn invalid_package_ids_do_not_write_files() -> Result<()> {
        let temporary = TestDirectory::new()?;
        let input = temporary.0.join("input.wasm");
        let output = temporary.0.join("package");
        fs::write(&input, Component::new().finish())?;
        let descriptor = descriptor("");
        let oversized = "a".repeat(129);
        for id in [
            "",
            " with-space",
            "with space",
            "line\nbreak",
            "caf\u{e9}",
            "back\\slash",
            "colon:id",
            oversized.as_str(),
        ] {
            let mut package = package(&descriptor);
            package.identity.id = id;
            assert!(package.metadata().is_err(), "{id:?}");
            assert!(package.write(&input, &output).is_err(), "{id:?}");
            assert!(!output.exists(), "{id:?}");
        }
        for id in [
            "a".to_string(),
            "a".repeat(128),
            "owner/package-1.2_3".to_string(),
        ] {
            let mut package = package(&descriptor);
            package.identity.id = &id;
            assert!(package.metadata().is_ok(), "{id:?}");
        }
        Ok(())
    }

    #[test]
    fn metadata_preserves_absent_and_declared_options_schemas() -> Result<()> {
        for (raw, expected) in [
            ("", None),
            (
                r#"{"type":"object","additionalProperties":false}"#,
                Some(json!({"type": "object", "additionalProperties": false})),
            ),
            ("false", Some(json!(false))),
            ("null", Some(serde_json::Value::Null)),
        ] {
            let descriptor = descriptor(raw);
            let metadata: serde_json::Value =
                serde_json::from_slice(&package(&descriptor).metadata()?)?;
            assert_eq!(
                metadata["descriptor"].get("options_schema"),
                expected.as_ref()
            );
        }
        Ok(())
    }

    #[test]
    fn packaging_writes_absent_and_declared_options_schemas() -> Result<()> {
        for (raw, expected) in [
            ("", json!({})),
            (
                r#"{"type":"object","additionalProperties":false}"#,
                json!({"type": "object", "additionalProperties": false}),
            ),
        ] {
            let temporary = TestDirectory::new()?;
            let input = temporary.0.join("input.wasm");
            let output = temporary.0.join("package");
            fs::write(&input, Component::new().finish())?;
            let descriptor = descriptor(raw);
            package(&descriptor).write(&input, &output)?;

            let schema: serde_json::Value =
                serde_json::from_slice(&fs::read(output.join("schema.json"))?)?;
            assert_eq!(schema, expected);
            let metadata = fs::read(output.join("metadata.json"))?;
            assert_eq!(metadata, package(&descriptor).metadata()?);
            let wasm = fs::read(output.join("plugin.wasm"))?;
            Validator::new().validate_all(&wasm)?;
            let mut embedded = Vec::new();
            for payload in Parser::new(0).parse_all(&wasm) {
                if let Payload::CustomSection(section) = payload?
                    && section.name() == METADATA_SECTION
                {
                    embedded.push(section.data().to_vec());
                }
            }
            assert_eq!(embedded, vec![metadata]);
            let checksums = fs::read_to_string(output.join("SHA256SUMS"))?;
            assert_eq!(checksums.lines().count(), 4);
            for line in checksums.lines() {
                let (checksum, name) = line.split_once("  ").ok_or("invalid checksum entry")?;
                assert_eq!(checksum, digest(&fs::read(output.join(name))?));
            }
        }
        Ok(())
    }

    #[test]
    fn malformed_options_schemas_do_not_write_package_files() -> Result<()> {
        let temporary = TestDirectory::new()?;
        let input = temporary.0.join("input.wasm");
        let output = temporary.0.join("package");
        fs::write(&input, Component::new().finish())?;
        for raw in [" ", "\n\t", "{", "not JSON"] {
            let descriptor = descriptor(raw);
            assert!(package(&descriptor).metadata().is_err());
            assert!(package(&descriptor).write(&input, &output).is_err());
            assert!(!output.exists());
        }
        Ok(())
    }

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
