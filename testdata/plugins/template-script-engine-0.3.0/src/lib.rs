//! Template-aware script-engine@0.3.0 fixture. It exports script-engine and
//! template-effects 0.3.0; the host calls the template-effects check. Each
//! check reports `TPL001` on the first character of the snippet with a fix
//! and the `line-length` effect unless the code is excluded. The `read`
//! option also reports a finding, with effect none, in a file of the granted
//! directory.
wit_bindgen::generate!({
    path: "../../../crates/actionlint-extension-api/wit/0.3.0",
    world: "template-script-engine-plugin",
});

use exports::kjanat::actionlint::script_engine::{
    self, Capabilities, Descriptor, Guest as ScriptGuest, ResourceKind, RuleDescriptor,
};
use exports::kjanat::actionlint::template_effects::{
    Diagnostic, Effect, Finding, Fix, Guest as TemplateGuest, Outcome, Position, Replacement,
    Request,
};
use serde_json::Value;

struct Plugin;

/// A finding on the first line of a file in the granted directory.
fn read(name: &str, request: &Request) -> Result<Finding, String> {
    let directory = request
        .directory
        .as_deref()
        .ok_or("no directory was granted")?;
    let text = std::fs::read_to_string(format!("{directory}/{name}"))
        .map_err(|error| format!("read {name}: {error}"))?;
    let line = text.lines().next().unwrap_or("");
    let width = u32::try_from(line.chars().count()).map_err(|error| error.to_string())?;
    Ok(Finding {
        diagnostic: Diagnostic {
            filename: name.to_owned(),
            code: "READ001".into(),
            name: String::new(),
            message: line.to_owned(),
            severity: "info".into(),
            location: Position { row: 1, column: 1 },
            end_location: Position {
                row: 1,
                column: width + 1,
            },
            url: None,
            source: Some(name.to_owned()),
            fixes: Vec::new(),
        },
        effect: Effect::None,
    })
}

impl ScriptGuest for Plugin {
    fn describe() -> Descriptor {
        Descriptor {
            id: "template-script-engine-0.3.0-fixture".into(),
            version: "1.0.0".into(),
            name: "Template script engine 0.3.0 fixture".into(),
            engine: "template".into(),
            engine_version: "1.0.0".into(),
            shells: vec!["bash".into()],
            rule: RuleDescriptor {
                name: "template".into(),
                description: "Report the first character of a snippet".into(),
                category: "correctness".into(),
                subcategory: "shell".into(),
                maturity: "stable".into(),
                provider: "template-script-engine-0.3.0-fixture".into(),
                recommended: true,
                explanation: String::new(),
                help: String::new(),
                documentation_url: "https://example.com/template".into(),
                options_schema: r#"{"type":"object"}"#.into(),
                examples: Vec::new(),
            },
            capabilities: Capabilities {
                fixes: true,
                reported_sources: true,
                resources: vec![ResourceKind::Directory],
            },
        }
    }

    fn validate_config(_config: String) -> Result<(), String> {
        Ok(())
    }

    fn check(_request: script_engine::Request) -> script_engine::Outcome {
        script_engine::Outcome {
            diagnostics: Vec::new(),
            error: Some("the host must call the template-effects check".into()),
        }
    }
}

impl TemplateGuest for Plugin {
    fn contract() -> String {
        "kjanat:actionlint/template-effects@0.1.0".into()
    }

    fn check(request: Request) -> Outcome {
        let options: Value = serde_json::from_str(&request.config).unwrap_or(Value::Null);
        let mut diagnostics = Vec::new();
        if !request.source.is_empty() && !request.excluded.iter().any(|code| code == "TPL001") {
            diagnostics.push(Finding {
                diagnostic: Diagnostic {
                    filename: request.filename.clone(),
                    code: "TPL001".into(),
                    name: String::new(),
                    message: "first character".into(),
                    severity: "style".into(),
                    location: Position { row: 1, column: 1 },
                    end_location: Position { row: 1, column: 2 },
                    url: None,
                    source: None,
                    fixes: vec![Fix {
                        description: "replace it".into(),
                        replacements: vec![Replacement {
                            location: Position { row: 1, column: 1 },
                            end_location: Position { row: 1, column: 2 },
                            text: "y".into(),
                        }],
                    }],
                },
                effect: Effect::LineLength,
            });
        }
        let mut error = None;
        if let Some(name) = options["read"].as_str() {
            match read(name, &request) {
                Ok(finding) => diagnostics.push(finding),
                Err(message) => error = Some(message),
            }
        }
        Outcome { diagnostics, error }
    }
}

export!(Plugin);
