use actionlint_extension_api::kjanat::actionlint::source_annotations as annotations;
use actionlint_extension_api::{Descriptor, Diagnostic, Fact, Guest, Input, Outcome, Position};

pub struct Plugin;

impl Guest for Plugin {
    fn describe() -> Descriptor {
        Descriptor {
            name: "source-annotations".into(),
            description: "Reports comments attached to supplied scripts".into(),
            category: "correctness".into(),
            subcategory: "scripts".into(),
            maturity: "experimental".into(),
            provider: "example".into(),
            recommended: false,
            explanation: "Exercises the optional source annotation host import.".into(),
            help: "Inspect the attached comment.".into(),
            documentation_url: String::new(),
            fix_availability: "none".into(),
            options_schema: "{}".into(),
            examples: Vec::new(),
            requires: vec![Fact::Scripts, Fact::Invocations],
        }
    }

    fn analyze(input: Input) -> Outcome {
        let mut diagnostics = Vec::new();
        for script in input.scripts.into_iter().chain(input.invoked) {
            let reference = annotations::SourceReference {
                document: script.reference.document.clone(),
                revision: script.reference.revision.clone(),
                node: script.reference.node.clone(),
                address: script.reference.address.clone(),
            };
            let mut unauthorized = reference.clone();
            unauthorized.revision.push_str("-unauthorized");
            if !annotations::comments(&unauthorized).is_empty() {
                return Outcome {
                    diagnostics,
                    error: Some("host returned comments for an unauthorized revision".into()),
                };
            }
            for comment in annotations::comments(&reference) {
                let position = Position {
                    line: comment.start.line,
                    column: comment.start.column,
                };
                diagnostics.push(Diagnostic {
                    reference: Some(script.reference.clone()),
                    explanation: String::new(),
                    help: String::new(),
                    documentation_url: String::new(),
                    related: Vec::new(),
                    rule: String::new(),
                    code: match comment.placement {
                        annotations::Placement::Leading => "leading",
                        annotations::Placement::Trailing => "trailing",
                    }
                    .into(),
                    severity: "warning".into(),
                    message: comment.text,
                    path: script.path.clone(),
                    start: position,
                    end: position,
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

#[cfg(target_arch = "wasm32")]
actionlint_extension_api::export_plugin!(Plugin);
