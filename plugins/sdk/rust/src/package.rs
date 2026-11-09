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

use crate::{
    ANALYZER_INTERFACE, ANALYZER_WIT_SHA256, Descriptor, InputKind, RUNNER_PROVIDER_INTERFACE,
    RUNNER_PROVIDER_WIT_SHA256,
};

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

/// Distribution of a snapshot-based runner provider. Supply the provider fields
/// from its `describe()` result; package identity and version are independent.
pub struct RunnerPackage<'a> {
    /// Package release metadata used by registry discovery.
    pub identity: Identity<'a>,
    /// Name returned by `describe()` and selected by `--runner-provider`.
    pub provider_name: &'a str,
    /// Implementation version from `describe()`, independent of the package release version.
    pub provider_version: &'a str,
    /// Execution platforms advertised by `describe()`, such as `buildkite`.
    pub targets: &'a [String],
    /// License file contents; CRLF line endings are converted to LF.
    pub license_text: &'a str,
}

/// A static action input that contains code for this engine.
#[derive(Serialize)]
pub struct ScriptInput<'a> {
    pub action: &'a str,
    pub input: &'a str,
    pub language: &'a str,
}

/// The world exported by a script engine component.
#[derive(Clone, Copy)]
pub enum ScriptWorld {
    /// The component exports only script-engine.
    ScriptEngine,
    /// The component also exports template-effects.
    TemplateScriptEngine,
}

/// Discovery metadata for a script engine, using its exported descriptor.
pub struct ScriptPackage<'a, D> {
    pub identity: Identity<'a>,
    pub descriptor: &'a D,
    pub world: ScriptWorld,
    pub script_inputs: &'a [ScriptInput<'a>],
    pub license_text: &'a str,
}

/// A script engine descriptor generated from a supported WIT world.
pub trait ScriptDescriptor {
    #[doc(hidden)]
    fn metadata(
        &self,
        identity: &Identity<'_>,
        inputs: &[ScriptInput<'_>],
    ) -> Result<serde_json::Value>;
    #[doc(hidden)]
    fn schema(&self) -> Result<Option<serde_json::Value>>;
}

impl<D: ScriptDescriptor> ScriptPackage<'_, D> {
    /// Serialize metadata that agrees with the descriptor returned by the guest.
    pub fn metadata(&self) -> Result<Vec<u8>> {
        let mut metadata = self
            .descriptor
            .metadata(&self.identity, self.script_inputs)?;
        if let ScriptWorld::TemplateScriptEngine = self.world {
            metadata["template_contract"] = json!("kjanat:actionlint/template-effects@0.1.0");
        }
        pretty_json(&metadata)
    }

    /// Write the component, metadata, schema, license and checksums.
    pub fn write(&self, input: &Path, output: &Path) -> Result<()> {
        let schema = self.descriptor.schema()?.unwrap_or_else(|| json!({}));
        write_distribution(
            input,
            output,
            self.metadata()?,
            pretty_json(&schema)?,
            self.license_text,
        )
    }
}

macro_rules! script_descriptor {
    ($module:path, $version:literal, $digest:expr, $capabilities:expr) => {
        const _: () = {
            use $module as generated;
            impl ScriptDescriptor for generated::Descriptor {
                fn schema(&self) -> Result<Option<serde_json::Value>> {
                    options_schema(&self.rule.options_schema)
                }

                fn metadata(&self, identity: &Identity<'_>, inputs: &[ScriptInput<'_>]) -> Result<serde_json::Value> {
                    if identity.id != self.id || identity.name != self.name || identity.version != self.version {
                        return Err("package identity must match the script descriptor".into());
                    }
                    let mut metadata = component_metadata(identity, concat!("kjanat:actionlint/script-engine@", $version), $digest)?;
                    let rule = &self.rule;
                    let schema = self.schema()?;
                    let examples: Vec<_> = rule.examples.iter().map(|example| json!({
                        "description": example.description, "before": example.before, "after": example.after
                    })).collect();
                    let mut rule_metadata = json!({
                        "name": rule.name, "description": rule.description, "category": rule.category,
                        "subcategory": rule.subcategory, "maturity": rule.maturity, "provider": rule.provider,
                        "recommended": rule.recommended, "explanation": rule.explanation, "help": rule.help,
                        "documentation_url": rule.documentation_url,
                    });
                    if let Some(schema) = &schema { rule_metadata["options_schema"] = schema.clone(); }
                    if !examples.is_empty() { rule_metadata["examples"] = json!(examples); }
                    metadata["engine"] = json!({"name": self.engine, "version": self.engine_version});
                    metadata["capabilities"] = $capabilities(self, inputs);
                    metadata["rules"] = json!([rule_metadata]);
                    if let Some(schema) = schema { metadata["config_schema"] = schema; }
                    Ok(metadata)
                }
            }
        };
    };
}

macro_rules! script_02 {
    ($module:path) => { script_descriptor!($module, "0.2.0", crate::SCRIPT_ENGINE_02_WIT_SHA256, |d: &generated::Descriptor, inputs: &[ScriptInput<'_>]| json!({
        "script_shells": d.shells, "script_inputs": inputs, "fixes": false, "format": false
    })); };
}
macro_rules! script_03 {
    ($module:path) => { script_descriptor!($module, "0.3.0", crate::SCRIPT_ENGINE_03_WIT_SHA256, |d: &generated::Descriptor, inputs: &[ScriptInput<'_>]| json!({
        "script_shells": d.shells, "script_inputs": inputs, "fixes": d.capabilities.fixes,
        "reported_sources": d.capabilities.reported_sources,
        "resources": d.capabilities.resources.iter().map(|resource| match resource { generated::ResourceKind::Directory => "directory" }).collect::<Vec<_>>(), "format": false
    })); };
}
script_02!(crate::script_engine::v0_2::exports::kjanat::actionlint0_2_0::script_engine);
script_03!(crate::script_engine::v0_3::exports::kjanat::actionlint::script_engine);

fn digest(bytes: &[u8]) -> String {
    Sha256::digest(bytes)
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect()
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

fn component_metadata(
    identity: &Identity<'_>,
    interface: &str,
    wit_sha256: &str,
) -> Result<serde_json::Value> {
    let id = identity.id;
    if !(1..=128).contains(&id.len())
        || !id
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'.' | b'_' | b'/' | b'-'))
    {
        return Err(
            "package id must contain 1 to 128 ASCII letters, digits, '.', '_', '/' or '-'".into(),
        );
    }
    Ok(json!({
        "metadata_version": 1,
        "id": id,
        "name": identity.name,
        "description": identity.description,
        "license": identity.license,
        "repository": identity.repository,
        "plugin_version": identity.version,
        "transport": "wasm-component",
        "interface": interface,
        "wit_sha256": wit_sha256,
    }))
}

impl Package<'_> {
    /// Serialize discovery metadata from the same descriptor the guest exports.
    pub fn metadata(&self) -> Result<Vec<u8>> {
        let mut metadata =
            component_metadata(&self.identity, ANALYZER_INTERFACE, ANALYZER_WIT_SHA256)?;
        let rule = self.descriptor;
        let required_inputs: Vec<_> = rule
            .required_inputs
            .iter()
            .map(|input_kind| match input_kind {
                InputKind::Source => "source",
                InputKind::Scripts => "scripts",
                InputKind::Mappings => "mappings",
                InputKind::Invocations => "invocations",
                InputKind::Execution => "execution",
                InputKind::Dependencies => "dependencies",
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
            "required_inputs": required_inputs,
        });
        if let Some(schema) = options_schema(&rule.options_schema)? {
            descriptor["options_schema"] = schema;
        }
        if !examples.is_empty() {
            descriptor["examples"] = json!(examples);
        }
        metadata["engine"] = serde_json::to_value(&self.engine)?;
        metadata["languages"] = json!(self.languages);
        metadata["capabilities"] = serde_json::to_value(&self.capabilities)?;
        metadata["descriptor"] = descriptor;
        pretty_json(&metadata)
    }

    /// Write the component, discovery/schema sidecars, license and SHA256SUMS.
    pub fn write(&self, input: &Path, output: &Path) -> Result<()> {
        let metadata = self.metadata()?;
        let schema = pretty_json(
            &options_schema(&self.descriptor.options_schema)?.unwrap_or_else(|| json!({})),
        )?;
        write_distribution(input, output, metadata, schema, self.license_text)
    }
}

impl RunnerPackage<'_> {
    /// Serialize the provider role and descriptor without starting the component.
    pub fn metadata(&self) -> Result<Vec<u8>> {
        let mut metadata = component_metadata(
            &self.identity,
            RUNNER_PROVIDER_INTERFACE,
            RUNNER_PROVIDER_WIT_SHA256,
        )?;
        metadata["descriptor"] = json!({
            "name": self.provider_name,
            "version": self.provider_version,
            "targets": self.targets,
        });
        pretty_json(&metadata)
    }

    /// Write the component, discovery manifest, empty option schema, license and
    /// checksums. Runner snapshots are supplied by the host, not package options.
    pub fn write(&self, input: &Path, output: &Path) -> Result<()> {
        write_distribution(
            input,
            output,
            self.metadata()?,
            pretty_json(&json!({}))?,
            self.license_text,
        )
    }
}

fn write_distribution(
    input: &Path,
    output: &Path,
    metadata: Vec<u8>,
    schema: Vec<u8>,
    license_text: &str,
) -> Result<()> {
    let wasm = with_metadata(&fs::read(input)?, &metadata)?;
    let files = [
        ("plugin.wasm", wasm),
        ("metadata.json", metadata),
        ("schema.json", schema),
        ("LICENSE", license_text.replace("\r\n", "\n").into_bytes()),
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
            maturity: "nursery".into(),
            provider: "example".into(),
            recommended: false,
            explanation: String::new(),
            help: String::new(),
            documentation_url: String::new(),
            fix_availability: String::new(), // Legacy ABI field; ignored by the host.
            options_schema: options_schema.into(),
            examples: Vec::new(),
            required_inputs: Vec::new(),
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
        let mut descriptor = descriptor("");
        descriptor.required_inputs = vec![InputKind::Source, InputKind::Dependencies];
        let metadata: serde_json::Value =
            serde_json::from_slice(&package(&descriptor).metadata()?)?;
        assert_eq!(
            metadata["descriptor"]["required_inputs"],
            serde_json::json!(["source", "dependencies"])
        );
        assert!(metadata["descriptor"].get("requires").is_none());
        assert_eq!(metadata["id"], "example-package");
        assert_eq!(metadata["name"], "Example");
        assert_eq!(metadata["descriptor"]["name"], descriptor.name);
        assert!(metadata["descriptor"].get("fix_availability").is_none());
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
    fn runner_packaging_preserves_provider_and_package_identity() -> Result<()> {
        let temporary = TestDirectory::new()?;
        let input = temporary.0.join("runner.wasm");
        let output = temporary.0.join("package");
        fs::write(&input, Component::new().finish())?;
        let targets = vec!["buildkite".to_string()];
        let package = RunnerPackage {
            identity: Identity {
                id: "example/queue-package",
                name: "Queue package",
                description: "Offline queue provider",
                license: "MIT",
                repository: "https://example.com/queue-package",
                version: "2.0.0",
            },
            provider_name: "queue-provider",
            provider_version: "1.0.0",
            targets: &targets,
            license_text: "Example license\r\n",
        };
        package.write(&input, &output)?;
        let metadata: serde_json::Value =
            serde_json::from_slice(&fs::read(output.join("metadata.json"))?)?;
        assert_eq!(metadata["id"], "example/queue-package");
        assert_eq!(metadata["plugin_version"], "2.0.0");
        assert_eq!(metadata["interface"], RUNNER_PROVIDER_INTERFACE);
        assert_eq!(metadata["wit_sha256"], RUNNER_PROVIDER_WIT_SHA256);
        assert_eq!(
            metadata["descriptor"],
            json!({"name": "queue-provider", "version": "1.0.0", "targets": ["buildkite"]})
        );
        assert_eq!(metadata["transport"], "wasm-component");
        assert!(metadata.get("engine").is_none());
        assert!(metadata["descriptor"].get("required_inputs").is_none());
        assert_eq!(
            fs::read_to_string(output.join("LICENSE"))?,
            "Example license\n"
        );
        assert_eq!(fs::read(output.join("metadata.json"))?, package.metadata()?);
        for line in fs::read_to_string(output.join("SHA256SUMS"))?.lines() {
            let (checksum, name) = line.split_once("  ").ok_or("invalid checksum entry")?;
            assert_eq!(checksum, digest(&fs::read(output.join(name))?));
        }
        let mut invalid = package;
        invalid.identity.id = "invalid package id";
        assert!(invalid.write(&input, &temporary.0.join("invalid")).is_err());
        assert!(!temporary.0.join("invalid").exists());
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

    #[test]
    fn script_engine_metadata_matches_supported_descriptors() -> Result<()> {
        use crate::script_engine::v0_2::exports::kjanat::actionlint0_2_0::script_engine::{
            Descriptor as Descriptor02, RuleDescriptor,
        };
        let descriptor = Descriptor02 {
            id: "example".into(),
            name: "Example".into(),
            version: "1.0.0".into(),
            engine: "check".into(),
            engine_version: "2.0.0".into(),
            shells: vec!["python".into()],
            rule: RuleDescriptor {
                name: "check".into(),
                description: "Check".into(),
                category: "correctness".into(),
                subcategory: "python".into(),
                maturity: "stable".into(),
                provider: "example".into(),
                recommended: true,
                explanation: String::new(),
                help: String::new(),
                documentation_url: String::new(),
                options_schema: r#"{"type":"object"}"#.into(),
                examples: vec![],
            },
        };
        let package = ScriptPackage {
            identity: Identity {
                id: "example",
                name: "Example",
                version: "1.0.0",
                description: "",
                license: "MIT",
                repository: "https://example.com",
            },
            descriptor: &descriptor,
            world: ScriptWorld::ScriptEngine,
            script_inputs: &[],
            license_text: "MIT",
        };
        let metadata: serde_json::Value = serde_json::from_slice(&package.metadata()?)?;
        assert_eq!(
            metadata["interface"],
            "kjanat:actionlint/script-engine@0.2.0"
        );
        assert_eq!(metadata["wit_sha256"], crate::SCRIPT_ENGINE_02_WIT_SHA256);
        assert_eq!(
            metadata["engine"],
            json!({"name":"check", "version":"2.0.0"})
        );
        assert_eq!(metadata["capabilities"]["script_shells"], json!(["python"]));
        assert_eq!(
            metadata["rules"][0]["options_schema"],
            json!({"type":"object"})
        );
        assert_eq!(metadata["config_schema"], json!({"type":"object"}));
        Ok(())
    }

    #[test]
    fn script_engine_03_metadata_includes_resources() -> Result<()> {
        use crate::script_engine::v0_3::exports::kjanat::actionlint::script_engine::{
            Capabilities as EngineCapabilities, Descriptor as Descriptor03, ResourceKind,
            RuleDescriptor,
        };
        let descriptor = Descriptor03 {
            id: "example".into(),
            name: "Example".into(),
            version: "1.0.0".into(),
            engine: "check".into(),
            engine_version: "2.0.0".into(),
            shells: vec!["bash".into()],
            rule: RuleDescriptor {
                name: "check".into(),
                description: String::new(),
                category: "correctness".into(),
                subcategory: String::new(),
                maturity: "stable".into(),
                provider: "example".into(),
                recommended: true,
                explanation: String::new(),
                help: String::new(),
                documentation_url: String::new(),
                options_schema: String::new(),
                examples: vec![],
            },
            capabilities: EngineCapabilities {
                fixes: true,
                reported_sources: true,
                resources: vec![ResourceKind::Directory],
            },
        };
        let package = ScriptPackage {
            identity: Identity {
                id: "example",
                name: "Example",
                version: "1.0.0",
                description: "",
                license: "MIT",
                repository: "https://example.com",
            },
            descriptor: &descriptor,
            world: ScriptWorld::ScriptEngine,
            script_inputs: &[],
            license_text: "MIT",
        };
        let metadata: serde_json::Value = serde_json::from_slice(&package.metadata()?)?;
        assert_eq!(
            metadata["interface"],
            "kjanat:actionlint/script-engine@0.3.0"
        );
        assert_eq!(metadata["wit_sha256"], crate::SCRIPT_ENGINE_03_WIT_SHA256);
        assert_eq!(metadata["capabilities"]["fixes"], true);
        assert_eq!(metadata["capabilities"]["reported_sources"], true);
        assert_eq!(metadata["capabilities"]["resources"], json!(["directory"]));
        Ok(())
    }

    #[test]
    fn script_contract_digests_match_sources() {
        for (source, expected) in [
            (
                crate::SCRIPT_ENGINE_02_WIT,
                crate::SCRIPT_ENGINE_02_WIT_SHA256,
            ),
            (
                crate::SCRIPT_ENGINE_03_WIT,
                crate::SCRIPT_ENGINE_03_WIT_SHA256,
            ),
            (
                crate::TEMPLATE_EFFECTS_02_WIT,
                crate::TEMPLATE_EFFECTS_02_WIT_SHA256,
            ),
            (
                crate::TEMPLATE_EFFECTS_03_WIT,
                crate::TEMPLATE_EFFECTS_03_WIT_SHA256,
            ),
        ] {
            assert_eq!(digest(source.replace("\r\n", "\n").as_bytes()), expected);
        }
    }

    // Reject a valid core module and a component with a truncated section payload.
    #[test]
    fn packaging_rejects_core_modules_and_truncated_components() {
        assert!(with_metadata(&Module::new().finish(), b"{}").is_err());
        assert!(with_metadata(&[0, 97, 115, 109, 13, 0, 1, 0, 1, 127], b"{}").is_err());
    }
}
