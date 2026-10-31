//! script-engine@0.3.0 fixture. It decodes ShellCheck `-f json1` output
//! passed as the `json1` option the way the bundled ShellCheck provider's
//! output program does, so the host can compare it with the process route.
//! The `read` option reports a finding in a file of the granted directory.
wit_bindgen::generate!({
    path: "../../../crates/actionlint-extension-api/wit/0.3.0",
    world: "script-engine-plugin",
});

use exports::kjanat::actionlint::script_engine::{
    Capabilities, Descriptor, Diagnostic, Fix, Guest, Outcome, Position, Replacement, Request,
    ResourceKind, RuleDescriptor,
};
use serde_json::Value;

struct Plugin;

fn number(value: &Value) -> u32 {
    value
        .as_u64()
        .and_then(|n| u32::try_from(n).ok())
        .unwrap_or(0)
}

fn position(value: &Value, row: &str, column: &str) -> Position {
    Position {
        row: number(&value[row]),
        column: number(&value[column]),
    }
}

/// One json1 comment, mapped like the bundled provider's output.jq.
fn comment(value: &Value, request: &Request) -> Option<Diagnostic> {
    let code = format!("SC{}", value["code"]);
    if request.excluded.contains(&code) {
        return None;
    }
    let file = value["file"].as_str().unwrap_or("");
    let in_snippet = file.is_empty() || file == "-";
    let message = value["message"].as_str().unwrap_or("").to_owned();
    let location = position(value, "line", "column");
    let end_location = if value["endLine"].is_null() {
        location
    } else {
        position(value, "endLine", "endColumn")
    };
    let replacements: Vec<Replacement> = value["fix"]["replacements"]
        .as_array()
        .map(|list| {
            list.iter()
                .map(|r| Replacement {
                    location: position(r, "line", "column"),
                    end_location: position(r, "endLine", "endColumn"),
                    text: r["replacement"].as_str().unwrap_or("").to_owned(),
                })
                .collect()
        })
        .unwrap_or_default();
    let fixes = if replacements.is_empty() {
        Vec::new()
    } else {
        vec![Fix {
            description: message.clone(),
            replacements,
        }]
    };
    Some(Diagnostic {
        filename: if in_snippet {
            request.filename.clone()
        } else {
            file.to_owned()
        },
        code,
        name: String::new(),
        message: message.strip_suffix('.').unwrap_or(&message).to_owned(),
        severity: value["level"].as_str().unwrap_or("").to_owned(),
        location,
        end_location,
        url: None,
        source: (!in_snippet).then(|| file.to_owned()),
        fixes,
    })
}

/// A finding on the first line of a file in the granted directory.
fn read(name: &str, request: &Request) -> Result<Diagnostic, String> {
    let directory = request
        .directory
        .as_deref()
        .ok_or("no directory was granted")?;
    let text = std::fs::read_to_string(format!("{directory}/{name}"))
        .map_err(|error| format!("read {name}: {error}"))?;
    let line = text.lines().next().unwrap_or("");
    let width = u32::try_from(line.chars().count()).map_err(|error| error.to_string())?;
    Ok(Diagnostic {
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
    })
}

impl Guest for Plugin {
    fn describe() -> Descriptor {
        Descriptor {
            id: "script-engine-0.3.0-fixture".into(),
            version: "1.0.0".into(),
            name: "Script engine 0.3.0 fixture".into(),
            engine: "json1".into(),
            engine_version: "1.0.0".into(),
            shells: vec!["bash".into()],
            rule: RuleDescriptor {
                name: "json1".into(),
                description: "Decode ShellCheck json1 output".into(),
                category: "correctness".into(),
                subcategory: "shell".into(),
                maturity: "stable".into(),
                provider: "script-engine-0.3.0-fixture".into(),
                recommended: true,
                explanation: String::new(),
                help: String::new(),
                documentation_url: "https://example.com/json1".into(),
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

    fn validate_config(config: String) -> Result<(), String> {
        let value: Value = serde_json::from_str(&config).map_err(|error| error.to_string())?;
        if !value.is_object() {
            return Err("options must be an object".into());
        }
        Ok(())
    }

    fn check(request: Request) -> Outcome {
        let options: Value = serde_json::from_str(&request.config).unwrap_or(Value::Null);
        let mut diagnostics: Vec<Diagnostic> = options["json1"]["comments"]
            .as_array()
            .map(|comments| {
                comments
                    .iter()
                    .filter_map(|value| comment(value, &request))
                    .collect()
            })
            .unwrap_or_default();
        let mut error = None;
        if let Some(name) = options["read"].as_str() {
            match read(name, &request) {
                Ok(diagnostic) => diagnostics.push(diagnostic),
                Err(message) => error = Some(message),
            }
        }
        Outcome { diagnostics, error }
    }
}

export!(Plugin);
