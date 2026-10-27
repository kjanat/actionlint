//! Concrete syntax and lowering into runtime-independent analysis facts.

use actionlint_model::{
    analysis::{Diagnostic, Severity},
    source::Snapshot,
};
use std::{error::Error, fmt};
use tree_sitter::{Parser, Tree};

mod expressions;
mod scalar;
pub use scalar::decode_scalar;

mod workflow;
pub use workflow::LoweredWorkflow;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum DocumentKind {
    Workflow,
    Action,
}

pub struct Document {
    source: Snapshot,
    tree: Tree,
    kind: DocumentKind,
}

#[derive(Debug)]
pub enum ParseError {
    Grammar(tree_sitter::LanguageError),
    Interrupted,
    Coordinates(actionlint_model::source::CoordinateError),
}

impl fmt::Display for ParseError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Grammar(error) => write!(f, "YAML grammar: {error}"),
            Self::Interrupted => f.write_str("YAML parsing did not complete"),
            Self::Coordinates(error) => write!(f, "YAML source location: {error}"),
        }
    }
}
impl Error for ParseError {}

impl Document {
    pub fn parse(source: Snapshot, kind: DocumentKind) -> Result<Self, ParseError> {
        let mut parser = Parser::new();
        parser
            .set_language(&tree_sitter_github_actions::LANGUAGE.into())
            .map_err(ParseError::Grammar)?;
        let tree = parser
            .parse(source.text(), None)
            .ok_or(ParseError::Interrupted)?;
        Ok(Self { source, tree, kind })
    }

    pub fn source(&self) -> &Snapshot {
        &self.source
    }

    pub fn diagnostics(&self) -> Result<Vec<Diagnostic>, ParseError> {
        let mut diagnostics = Vec::new();
        let mut pending = vec![self.tree.root_node()];
        while let Some(node) = pending.pop() {
            let message = if node.is_error() || node.is_missing() {
                Some("Invalid YAML syntax.")
            } else if self.kind == DocumentKind::Action && matches!(node.kind(), "anchor" | "alias")
            {
                Some("Action manifests do not support YAML anchors or aliases.")
            } else {
                None
            };
            if let Some(message) = message {
                diagnostics.push(Diagnostic {
                    related: Vec::new(),
                    fixes: Vec::new(),
                    rule: "syntax/yaml".into(),
                    message: message.into(),
                    severity: Severity::Error,
                    primary: self
                        .source
                        .span(node.byte_range())
                        .map_err(ParseError::Coordinates)?,
                });
                if node.is_error() {
                    continue;
                }
            }
            let mut cursor = node.walk();
            let children: Vec<_> = node.children(&mut cursor).collect();
            pending.extend(children.into_iter().rev());
        }
        Ok(diagnostics)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(text: &str, kind: DocumentKind) -> Document {
        Document::parse(Snapshot::new("test.yml".into(), text.into()), kind).unwrap()
    }

    #[test]
    fn document_retains_original_text_and_repeated_keys() {
        let text = "\u{feff}# comment\r\njobs:\r\n  build: {}\r\n  build: {}\r\n";
        let document = parse(text, DocumentKind::Workflow);
        assert_eq!(document.source().text(), text);
        assert!(document.diagnostics().unwrap().is_empty());
        let mut pending = vec![document.tree.root_node()];
        let mut count = 0;
        while let Some(node) = pending.pop() {
            if node.kind() == "string_scalar" && &text[node.byte_range()] == "build" {
                count += 1;
            }
            let mut cursor = node.walk();
            pending.extend(node.named_children(&mut cursor));
        }
        assert_eq!(count, 2);
    }

    #[test]
    fn action_and_workflow_alias_rules_differ() {
        let text = "name: &name example\nruns: *name\n";
        assert!(
            parse(text, DocumentKind::Workflow)
                .diagnostics()
                .unwrap()
                .is_empty()
        );
        let action = parse(text, DocumentKind::Action);
        let diagnostics = action.diagnostics().unwrap();
        assert_eq!(
            diagnostics
                .iter()
                .map(|d| d.primary.text())
                .collect::<Vec<_>>(),
            ["&name", "*name"]
        );
    }

    #[test]
    fn broken_editor_input_has_a_tree_and_an_error() {
        let document = parse("jobs: [unterminated", DocumentKind::Workflow);
        assert!(!document.diagnostics().unwrap().is_empty());
        assert_eq!(document.source().text(), "jobs: [unterminated");
    }
}
