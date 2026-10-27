use std::collections::HashSet;

use actionlint_model::{
    analysis::{Diagnostic, Fact, Severity, Unavailable},
    source::Span,
    workflow::{Job, Name, Workflow},
};
use tree_sitter::Node;

use crate::{Document, DocumentKind, ParseError};

pub struct LoweredWorkflow {
    pub workflow: Workflow,
    pub diagnostics: Vec<Diagnostic>,
}

fn children(node: Node<'_>) -> Vec<Node<'_>> {
    let mut cursor = node.walk();
    node.named_children(&mut cursor)
        .filter(|child| child.kind() != "comment")
        .collect()
}

struct Lowerer<'a> {
    document: &'a Document,
    anchors: Vec<Node<'a>>,
    diagnostics: Vec<Diagnostic>,
}

impl Document {
    pub fn workflow(&self) -> Result<LoweredWorkflow, ParseError> {
        let diagnostics = self.diagnostics()?;
        if self.kind != DocumentKind::Workflow || !diagnostics.is_empty() {
            return Ok(LoweredWorkflow {
                workflow: Workflow {
                    jobs: Fact::Unavailable(if self.kind == DocumentKind::Workflow {
                        Unavailable::Invalid
                    } else {
                        Unavailable::Unsupported
                    }),
                },
                diagnostics,
            });
        }
        let mut lowerer = Lowerer {
            document: self,
            anchors: Vec::new(),
            diagnostics,
        };
        let mut pending = vec![self.tree.root_node()];
        while let Some(node) = pending.pop() {
            if node.kind() == "anchor" {
                lowerer.anchors.push(node);
            }
            pending.extend(children(node).into_iter().rev());
        }
        let jobs = match lowerer.jobs() {
            Ok(jobs) => Fact::Known(jobs),
            Err(reason) => Fact::Unavailable(reason),
        };
        Ok(LoweredWorkflow {
            workflow: Workflow { jobs },
            diagnostics: lowerer.diagnostics,
        })
    }
}

impl<'a> Lowerer<'a> {
    fn text(&self, node: Node<'a>) -> &str {
        &self.document.source.text()[node.byte_range()]
    }

    fn location(&self, node: Node<'a>) -> Result<Span, Unavailable> {
        self.document
            .source
            .span(node.byte_range())
            .map_err(|_| Unavailable::Invalid)
    }

    fn invalid(&mut self, node: Node<'a>, message: &str) -> Unavailable {
        if let Ok(primary) = self.location(node) {
            self.diagnostics.push(Diagnostic {
                related: Vec::new(),
                fixes: Vec::new(),
                rule: "syntax/workflow".into(),
                message: message.into(),
                severity: Severity::Error,
                primary,
            });
        }
        Unavailable::Invalid
    }

    fn resolve(&self, mut node: Node<'a>) -> Result<(Node<'a>, Option<Span>), Unavailable> {
        let mut aliases = HashSet::new();
        let mut location = None;
        for _ in 0..256 {
            match node.kind() {
                "document" | "block_node" | "flow_node" | "block_sequence_item" => {
                    let children = children(node);
                    if children.iter().any(|child| child.kind() == "tag") {
                        return Err(Unavailable::Unsupported);
                    }
                    node = children
                        .into_iter()
                        .find(|child| {
                            !matches!(
                                child.kind(),
                                "anchor"
                                    | "yaml_directive"
                                    | "tag_directive"
                                    | "reserved_directive"
                            )
                        })
                        .ok_or(Unavailable::Invalid)?;
                }
                "alias" => {
                    if !aliases.insert(node.id()) {
                        return Err(Unavailable::Invalid);
                    }
                    if location.is_none() {
                        location = Some(self.location(node)?);
                    }
                    let name = self
                        .text(node)
                        .strip_prefix('*')
                        .ok_or(Unavailable::Invalid)?;
                    let anchor = self
                        .anchors
                        .iter()
                        .rev()
                        .find(|anchor| {
                            anchor.start_byte() < node.start_byte()
                                && self.text(**anchor).strip_prefix('&') == Some(name)
                        })
                        .ok_or(Unavailable::Invalid)?;
                    node = anchor.parent().ok_or(Unavailable::Invalid)?;
                }
                _ => return Ok((node, location)),
            }
        }
        Err(Unavailable::BudgetExceeded)
    }

    fn name(&self, node: Node<'a>) -> Result<Name, Unavailable> {
        let (scalar, alias) = self.resolve(node)?;
        if !matches!(
            scalar.kind(),
            "plain_scalar" | "single_quote_scalar" | "double_quote_scalar" | "block_scalar"
        ) {
            return Err(Unavailable::Invalid);
        }
        let decoded =
            crate::decode_scalar(self.location(scalar)?, crate::scalar::parent_indent(scalar))?;
        Ok(Name {
            value: decoded.text().into(),
            location: alias.unwrap_or(self.location(scalar)?),
        })
    }

    fn pairs(&self, node: Node<'a>) -> Result<Vec<Node<'a>>, Unavailable> {
        let (node, _) = self.resolve(node)?;
        if !matches!(node.kind(), "block_mapping" | "flow_mapping") {
            return Err(Unavailable::Invalid);
        }
        Ok(children(node))
    }

    fn field(&mut self, node: Node<'a>, key: &str) -> Result<Option<Node<'a>>, Unavailable> {
        let mut found = None;
        for pair in self.pairs(node)? {
            let name = self.name(
                pair.child_by_field_name("key")
                    .ok_or(Unavailable::Invalid)?,
            )?;
            if name.value == "<<" {
                return Err(Unavailable::Unsupported);
            }
            if name.value != key {
                continue;
            }
            if found.is_some() {
                return Err(self.invalid(pair, &format!("Duplicate {key:?} field.")));
            }
            found = Some(
                pair.child_by_field_name("value")
                    .ok_or_else(|| self.invalid(pair, &format!("Missing value for {key:?}.")))?,
            );
        }
        Ok(found)
    }

    fn needs(&mut self, node: Node<'a>) -> Result<Vec<Name>, Unavailable> {
        let (value, alias) = self.resolve(node)?;
        let items = match value.kind() {
            "block_sequence" | "flow_sequence" => children(value),
            _ => vec![node],
        };
        let mut names = Vec::new();
        for item in items {
            let mut name = self.name(item)?;
            if name.value.contains("${{") {
                return Err(Unavailable::Unresolved);
            }
            if let Some(location) = &alias {
                name.location = location.clone();
            }
            names.push(name);
        }
        Ok(names)
    }

    fn jobs(&mut self) -> Result<Vec<Job>, Unavailable> {
        let documents = children(self.document.tree.root_node());
        if documents.len() != 1 {
            return Err(self.invalid(
                self.document.tree.root_node(),
                "Expected one workflow document.",
            ));
        }
        let root = documents[0];
        let jobs = self
            .field(root, "jobs")?
            .ok_or_else(|| self.invalid(root, "Workflow requires a jobs mapping."))?;
        let mut names = HashSet::new();
        let mut result = Vec::new();
        for pair in self.pairs(jobs)? {
            let key = pair
                .child_by_field_name("key")
                .ok_or(Unavailable::Invalid)?;
            let name = self.name(key)?;
            if !names.insert(name.value.to_lowercase()) {
                return Err(self.invalid(key, "Duplicate job identifier."));
            }
            let body = pair
                .child_by_field_name("value")
                .ok_or_else(|| self.invalid(pair, "Job requires a mapping."))?;
            let needs = match self.field(body, "needs") {
                Ok(Some(node)) => match self.needs(node) {
                    Ok(names) => Fact::Known(names),
                    Err(reason) => {
                        if reason == Unavailable::Invalid {
                            self.invalid(node, "Expected a job name or a sequence of job names.");
                        }
                        Fact::Unavailable(reason)
                    }
                },
                Ok(None) => Fact::Known(Vec::new()),
                Err(reason) => Fact::Unavailable(reason),
            };
            result.push(Job { name, needs });
        }
        Ok(result)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use actionlint_model::source::Snapshot;

    fn lower(text: &str) -> LoweredWorkflow {
        Document::parse(
            Snapshot::new("workflow.yml".into(), text.into()),
            DocumentKind::Workflow,
        )
        .unwrap()
        .workflow()
        .unwrap()
    }

    #[test]
    fn block_and_flow_dependencies_decode_without_losing_source_tokens() {
        let result = lower("jobs:\n  build: {}\n  test:\n    needs: [build, \"missing\"]\n");
        assert!(result.diagnostics.is_empty());
        let Fact::Known(jobs) = result.workflow.jobs else {
            panic!("expected jobs")
        };
        let Fact::Known(needs) = &jobs[1].needs else {
            panic!("expected needs")
        };
        assert_eq!(needs[1].value, "missing");
        assert_eq!(needs[1].location.text(), "\"missing\"");
    }

    #[test]
    fn explicit_scalar_indentation_uses_declaration_context() {
        for value in ["|2-\n      build", ">2-\n      build"] {
            let result = lower(&format!(
                "jobs:\n  build: {{}}\n  test:\n    needs: {value}\n"
            ));
            let Fact::Known(jobs) = result.workflow.jobs else {
                panic!("expected jobs")
            };
            let Fact::Known(needs) = &jobs[1].needs else {
                panic!("expected needs")
            };
            assert_eq!(needs[0].value, "build");
        }
    }

    #[test]
    fn explicit_indentation_survives_sequence_and_alias_resolution() {
        let result = lower(
            "jobs:\n  build: {}\n  test:\n    needs:\n      - &dependency |2-\n        build\n  reused:\n    needs: *dependency\n",
        );
        let Fact::Known(jobs) = result.workflow.jobs else {
            panic!("expected jobs")
        };
        for job in &jobs[1..] {
            let Fact::Known(needs) = &job.needs else {
                panic!("expected needs")
            };
            assert_eq!(needs[0].value, "build");
            if job.name.value == "reused" {
                assert_eq!(needs[0].location.text(), "*dependency");
            }
        }
    }

    #[test]
    fn alias_dependencies_point_at_use_site() {
        let result = lower("jobs:\n  build: {needs: &deps [missing]}\n  test: {needs: *deps}\n");
        let Fact::Known(jobs) = result.workflow.jobs else {
            panic!("expected jobs")
        };
        let Fact::Known(needs) = &jobs[1].needs else {
            panic!("expected alias needs")
        };
        assert_eq!(needs[0].value, "missing");
        assert_eq!(needs[0].location.text(), "*deps");
    }

    #[test]
    fn duplicate_job_names_are_not_overwritten() {
        let result = lower("jobs: {build: {}, BUILD: {}}\n");
        assert!(matches!(
            result.workflow.jobs,
            Fact::Unavailable(Unavailable::Invalid)
        ));
        assert_eq!(result.diagnostics[0].primary.text(), "BUILD");
    }

    #[test]
    fn invalid_and_dynamic_dependencies_cannot_become_empty_lists() {
        for (value, expected) in [
            ("42", Unavailable::Invalid),
            ("'${{ inputs.job }}'", Unavailable::Unresolved),
            ("*missing", Unavailable::Invalid),
        ] {
            let result = lower(&format!("jobs: {{test: {{needs: {value}}}}}\n"));
            let Fact::Known(jobs) = result.workflow.jobs else {
                panic!("expected job declaration")
            };
            assert_eq!(jobs[0].needs, Fact::Unavailable(expected));
        }
    }
}
