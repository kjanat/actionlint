use actionlint_extension_api::exports::kjanat::actionlint::tool_integration;
use actionlint_extension_api::{Descriptor, Diagnostic, Fact, Guest, Input, Outcome, Position};

struct Plugin;
impl Guest for Plugin {
    fn describe() -> Descriptor {
        Descriptor {
            name: "shellcheck".into(),
            description: "Managed integration fixture".into(),
            category: "correctness".into(),
            subcategory: "scripts".into(),
            maturity: "stable".into(),
            provider: "fixture".into(),
            recommended: false,
            explanation: String::new(),
            help: String::new(),
            documentation_url: String::new(),
            fix_availability: "none".into(),
            options_schema: "{}".into(),
            examples: Vec::new(),
            requires: vec![Fact::Scripts],
        }
    }
    fn analyze(input: Input) -> Outcome {
        let mut diagnostics = Vec::new();
        for script in input.scripts {
            for (index, line) in script.text.lines().enumerate() {
                let Some(column) = line.find("sudo") else {
                    continue;
                };
                let column = line[..column].chars().count() as u32 + 1;
                let Some(span) = script.spans.iter().find(|span| {
                    span.line == index as u32 + 1 && span.start <= column && column < span.end
                }) else {
                    continue;
                };
                let start = Position {
                    line: span.document.line,
                    column: span.document.column + column - span.start,
                };
                let end = Position {
                    line: start.line,
                    column: start.column + 4,
                };
                diagnostics.push(Diagnostic {
                    reference: Some(script.reference.clone()),
                    rule: "shellcheck".into(),
                    code: "fixture".into(),
                    severity: "warning".into(),
                    message: "sudo found".into(),
                    path: script.path.clone(),
                    start,
                    end,
                    explanation: String::new(),
                    help: String::new(),
                    documentation_url: String::new(),
                    related: Vec::new(),
                    fixes: Vec::new(),
                });
            }
        }
        Outcome {
            diagnostics,
            error: None,
        }
    }
}
impl tool_integration::Guest for Plugin {
    fn describe() -> tool_integration::Descriptor {
        tool_integration::Descriptor {
            tool: "shellcheck".into(),
            contract: "kjanat:actionlint/shellcheck-config@0.1.0".into(),
            configuration_schema: actionlint_extension_api::SHELLCHECK_CONFIGURATION_SCHEMA.into(),
        }
    }
    fn negotiate(
        _config: String,
        _options: String,
    ) -> Result<tool_integration::Negotiated, String> {
        Ok(tool_integration::Negotiated {
            options: "{}".into(),
            resources: Vec::new(),
        })
    }
}
actionlint_extension_api::export_plugin!(Plugin);
