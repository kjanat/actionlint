//! Inspect YAML comments attached to scripts through the optional host interface.
//! Comments inside a script scalar are script text, not YAML annotations.

use actionlint_extension_api::kjanat::actionlint::source_annotations as annotations;
use actionlint_extension_api::{
    Descriptor, Diagnostic, Guest, Input, InputKind, Outcome, Position,
};

pub struct Plugin;

impl Guest for Plugin {
    fn describe() -> Descriptor {
        Descriptor {
            name: "source-annotations".into(),
            description: "Reports comments attached to supplied scripts".into(),
            category: "correctness".into(),
            subcategory: "scripts".into(),
            maturity: String::new(),
            provider: "example".into(),
            recommended: false,
            explanation: "Reports each YAML comment attached to a supplied script declaration.".into(),
            help: "Inspect the attached comment.".into(),
            documentation_url: "https://github.com/kjanat/actionlint/tree/master/plugins/examples/source-annotations".into(),
            fix_availability: String::new(), // Legacy ABI field; ignored by the host.
            options_schema: "{}".into(),
            examples: Vec::new(),
            required_inputs: vec![InputKind::Scripts, InputKind::Invocations],
        }
    }

    fn analyze(input: Input) -> Outcome {
        analyze_comments(input, annotations::comments)
    }
}

fn analyze_comments(
    input: Input,
    comments: impl Fn(&annotations::SourceReference) -> Vec<annotations::Comment>,
) -> Outcome {
    let mut diagnostics = Vec::new();
    for script in input.scripts.into_iter().chain(input.invoked) {
        let reference = annotations::SourceReference {
            document: script.reference.document.clone(),
            revision: script.reference.revision.clone(),
            node: script.reference.node.clone(),
            address: script.reference.address.clone(),
        };
        for comment in comments(&reference) {
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

#[cfg(target_arch = "wasm32")]
actionlint_extension_api::export_plugin!(Plugin);

#[cfg(test)]
mod tests {
    use super::*;
    use actionlint_extension_api::{Script, SourceReference};
    use std::cell::Cell;

    fn input() -> Input {
        Input {
            reference: SourceReference {
                document: "workflow".into(),
                revision: "captured-workflow".into(),
                node: "root".into(),
                address: String::new(),
            },
            path: "workflow.yml".into(),
            content: "not YAML: [".into(),
            kind: "workflow".into(),
            parse_status: "parsed".into(),
            available_inputs: vec![InputKind::Scripts, InputKind::Invocations],
            options: String::new(),
            scripts: Vec::new(),
            invoked: Vec::new(),
            mappings: Vec::new(),
            dependencies: Vec::new(),
            selected_scripts: None,
            action_inputs: Vec::new(),
        }
    }

    fn script(node: &str, path: &str) -> Script {
        Script {
            reference: SourceReference {
                document: path.into(),
                revision: format!("captured-{node}"),
                node: node.into(),
                address: "runs.steps[0]".into(),
            },
            job_id: String::new(),
            step_id: String::new(),
            text: "# comment inside a scalar\necho hello".into(),
            shell: "bash".into(),
            start: Position { line: 8, column: 9 },
            path: path.into(),
            spans: Vec::new(),
            occurrence: None,
            runtime: None,
        }
    }

    #[test]
    fn reports_attached_comments_for_supplied_and_invoked_scripts() {
        let mut input = input();
        input.scripts.push(script("root", "workflow.yml"));
        input.invoked.push(script("called", "inner/action.yml"));
        let queries = Cell::new(0);
        let outcome = analyze_comments(input, |reference| {
            queries.set(queries.get() + 1);
            assert_eq!(reference.revision, format!("captured-{}", reference.node));
            let (text, placement, line, column) = match reference.node.as_str() {
                "root" => ("# naïve", annotations::Placement::Leading, 3, 7),
                "called" => ("# called", annotations::Placement::Trailing, 6, 19),
                node => panic!("queried an unsupplied reference: {node}"),
            };
            vec![annotations::Comment {
                text: text.into(),
                placement,
                start: annotations::Position { line, column },
            }]
        });
        assert_eq!(queries.get(), 2);
        assert!(outcome.error.is_none());
        assert_eq!(outcome.diagnostics.len(), 2);
        for (finding, (node, path, text, code, line, column)) in outcome.diagnostics.iter().zip([
            ("root", "workflow.yml", "# naïve", "leading", 3, 7),
            ("called", "inner/action.yml", "# called", "trailing", 6, 19),
        ]) {
            let reference = finding.reference.as_ref().expect("captured reference");
            assert_eq!(reference.document, path);
            assert_eq!(reference.revision, format!("captured-{node}"));
            assert_eq!(reference.node, node);
            assert_eq!(reference.address, "runs.steps[0]");
            assert_eq!(finding.path, path);
            assert_eq!(finding.message, text);
            assert_eq!(finding.code, code);
            assert_eq!(finding.severity, "warning");
            assert_eq!((finding.start.line, finding.start.column), (line, column));
            assert_eq!((finding.end.line, finding.end.column), (line, column));
            assert!(finding.fixes.is_empty());
        }
    }

    #[test]
    fn script_text_does_not_supply_yaml_comments() {
        let mut input = input();
        input.scripts.push(script("root", "workflow.yml"));
        let outcome = analyze_comments(input, |_| Vec::new());
        assert!(outcome.error.is_none());
        assert!(outcome.diagnostics.is_empty());
    }

    #[test]
    fn empty_input_does_not_query_the_host() {
        let outcome = analyze_comments(input(), |_| panic!("no scripts were supplied"));
        assert!(outcome.error.is_none());
        assert!(outcome.diagnostics.is_empty());
    }
}
