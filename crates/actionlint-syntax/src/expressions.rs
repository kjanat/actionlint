use crate::{Document, ParseError};
use actionlint_model::expression::{Expression, ExpressionKind};
use actionlint_model::source::{MappedText, Origin};

impl Document {
    /// Outermost expression regions in original source order, without evaluating them.
    pub fn expressions(&self) -> Result<Vec<Expression>, ParseError> {
        let mut sites = Vec::new();
        let mut pending = vec![self.tree.root_node()];
        while let Some(node) = pending.pop() {
            if matches!(
                node.kind(),
                "plain_scalar"
                    | "single_quote_scalar"
                    | "double_quote_scalar"
                    | "block_scalar"
                    | "expression"
            ) {
                let location = self
                    .source
                    .span(node.byte_range())
                    .map_err(ParseError::Coordinates)?;
                let decoded = if node.kind() == "expression" {
                    let mut text = MappedText::default();
                    text.copy(location.clone());
                    Some(text)
                } else {
                    crate::decode_scalar(location.clone(), crate::scalar::parent_indent(node)).ok()
                };
                if let Some(decoded) = decoded {
                    let condition = is_condition(node, self);
                    for (kind, selected) in regions(decoded.text(), condition) {
                        let text = decoded.slice(selected).map_err(ParseError::Coordinates)?;
                        let origins = text
                            .origins(0..text.text().len())
                            .map_err(ParseError::Coordinates)?;
                        let mut bounds = None::<std::ops::Range<usize>>;
                        for origin in origins {
                            if let Origin::Exact(span) | Origin::Transformed(span) = origin {
                                bounds = Some(match bounds {
                                    None => span.bytes(),
                                    Some(existing) => {
                                        existing.start.min(span.bytes().start)
                                            ..existing.end.max(span.bytes().end)
                                    }
                                });
                            }
                        }
                        let location = match bounds {
                            Some(bounds) => {
                                self.source.span(bounds).map_err(ParseError::Coordinates)?
                            }
                            None => location.clone(),
                        };
                        sites.push(Expression {
                            kind,
                            location,
                            text,
                        });
                    }
                }
                continue;
            }
            if node.is_error() {
                continue;
            }
            let mut cursor = node.walk();
            let children: Vec<_> = node.named_children(&mut cursor).collect();
            pending.extend(children.into_iter().rev());
        }
        Ok(sites)
    }
}

enum PathPart {
    Field(String),
    Item,
}

fn is_condition(mut node: tree_sitter::Node<'_>, document: &Document) -> bool {
    let mut path = Vec::new();
    while let Some(parent) = node.parent() {
        if let Some(value) = parent.child_by_field_name("value") {
            if value.id() != node.id() {
                return false;
            }
            let Some(key) = parent.child_by_field_name("key") else {
                return false;
            };
            let Ok(location) = document.source.span(key.byte_range()) else {
                return false;
            };
            let Ok(decoded) = crate::decode_scalar(location, crate::scalar::parent_indent(key))
            else {
                return false;
            };
            path.push(PathPart::Field(decoded.text().into()));
        } else if matches!(parent.kind(), "block_sequence" | "flow_sequence") {
            path.push(PathPart::Item);
        }
        node = parent;
    }
    use PathPart::{Field, Item};
    match (document.kind, path.as_slice()) {
        (crate::DocumentKind::Workflow, [Field(condition), Field(_job), Field(jobs)]) => {
            condition == "if" && jobs == "jobs"
        }
        (
            crate::DocumentKind::Workflow,
            [
                Field(condition),
                Item,
                Field(steps),
                Field(_job),
                Field(jobs),
            ],
        ) => condition == "if" && steps == "steps" && jobs == "jobs",
        (crate::DocumentKind::Action, [Field(condition), Item, Field(steps), Field(runs)]) => {
            condition == "if" && steps == "steps" && runs == "runs"
        }
        _ => false,
    }
}

fn regions(text: &str, condition: bool) -> Vec<(ExpressionKind, std::ops::Range<usize>)> {
    let mut result = Vec::new();
    let mut offset = 0;
    while let Some(relative) = text[offset..].find("${{") {
        let start = offset + relative;
        let mut cursor = start + 3;
        let mut quoted = false;
        let mut end = None;
        while cursor < text.len() {
            if text.as_bytes()[cursor] == b'\'' {
                if quoted && text.as_bytes().get(cursor + 1) == Some(&b'\'') {
                    cursor += 2;
                    continue;
                }
                quoted = !quoted;
            } else if !quoted && text[cursor..].starts_with("}}") {
                end = Some(cursor + 2);
                break;
            }
            cursor += text[cursor..].chars().next().map_or(1, char::len_utf8);
        }
        match end {
            Some(end) => {
                result.push((ExpressionKind::Delimited, start..end));
                offset = end;
            }
            None => {
                result.push((ExpressionKind::Unterminated, start..text.len()));
                break;
            }
        }
    }
    if result.is_empty() && condition && !text.is_empty() {
        result.push((ExpressionKind::BareCondition, 0..text.len()));
    }
    result
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::DocumentKind;
    use actionlint_model::source::Snapshot;

    #[test]
    fn finds_real_expression_nodes_without_scanning_comments() {
        let text = "# ${{ not.an.expression }}\r\njobs:\r\n  build:\r\n    if: github.ref == 'refs/heads/main'\r\n    runs-on: ${{ matrix.os }}\r\n    name: \"🦀-${{ github.actor }}\"\r\n";
        let source = Snapshot::new("workflow.yml".into(), text.into());
        let document = Document::parse(source.clone(), DocumentKind::Workflow).unwrap();
        assert!(document.diagnostics().unwrap().is_empty());
        let sites = document.expressions().unwrap();
        assert_eq!(
            sites
                .iter()
                .map(|site| (site.kind, site.location.text()))
                .collect::<Vec<_>>(),
            vec![
                (
                    ExpressionKind::BareCondition,
                    "github.ref == 'refs/heads/main'"
                ),
                (ExpressionKind::Delimited, "${{ matrix.os }}"),
                (ExpressionKind::Delimited, "${{ github.actor }}"),
            ]
        );
        assert!(sites.iter().all(|site| site.location.source() == &source));
    }

    #[test]
    fn yaml_escapes_can_introduce_expression_delimiters() {
        let source = Snapshot::new(
            "workflow.yml".into(),
            "jobs:\n  build:\n    name: \"\\u0024{{ github.actor }}\"\n".into(),
        );
        let document = Document::parse(source.clone(), DocumentKind::Workflow).unwrap();
        let expressions = document.expressions().unwrap();
        assert_eq!(expressions.len(), 1);
        assert_eq!(expressions[0].text.text(), "${{ github.actor }}");
        let offset = source.text().find("\\u0024").unwrap();
        assert_eq!(
            expressions[0].text.origins(0..1).unwrap(),
            vec![Origin::Transformed(
                source.span(offset..offset + 6).unwrap()
            )]
        );
    }

    #[test]
    fn folded_expressions_and_quoted_conditions_use_decoded_text() {
        let source = Snapshot::new("workflow.yml".into(), "jobs:\n  build:\n    if: 'github.ref == ''main'''\n    name: >-\n      ${{ github.actor\n      }}\n".into());
        let document = Document::parse(source, DocumentKind::Workflow).unwrap();
        let expressions = document.expressions().unwrap();
        assert_eq!(
            expressions
                .iter()
                .map(|site| (site.kind, site.text.text()))
                .collect::<Vec<_>>(),
            vec![
                (ExpressionKind::BareCondition, "github.ref == 'main'"),
                (ExpressionKind::Delimited, "${{ github.actor }}"),
            ]
        );
        assert!(
            expressions[1]
                .text
                .origins(0..expressions[1].text.text().len())
                .unwrap()
                .iter()
                .any(|origin| matches!(origin, Origin::Transformed(_)))
        );
    }

    #[test]
    fn expression_string_literals_do_not_close_the_outer_delimiter() {
        let text = "${{ format('a''}}{0}', github.actor) }} / ${{ inputs.name }}";
        let found = regions(text, false);
        assert_eq!(found.len(), 2);
        assert_eq!(
            &text[found[0].1.clone()],
            "${{ format('a''}}{0}', github.actor) }}"
        );
        assert_eq!(&text[found[1].1.clone()], "${{ inputs.name }}");
        assert_eq!(
            regions("prefix ${{ 'unterminated", false),
            vec![(ExpressionKind::Unterminated, 7..24)]
        );
    }

    #[test]
    fn nested_expression_nodes_do_not_duplicate_regions() {
        let source = Snapshot::new(
            "workflow.yml".into(),
            "jobs:\n  build:\n    if: success() && (github.ref == 'main' || cancelled())\n".into(),
        );
        let document = Document::parse(source, DocumentKind::Workflow).unwrap();
        assert!(document.diagnostics().unwrap().is_empty());
        let sites = document.expressions().unwrap();
        assert_eq!(sites.len(), 1);
        assert_eq!(
            sites[0].location.text(),
            "success() && (github.ref == 'main' || cancelled())"
        );
    }

    #[test]
    fn bare_conditions_require_a_job_or_step_field() {
        let source = Snapshot::new(
            "workflow.yml".into(),
            r#"
if: root_value
env:
  if: environment_value
jobs:
  build:
    if: job_condition
    env:
      if: job_environment
    strategy:
      matrix:
        include:
          - if: matrix_value
    steps:
      - if: step_condition
        with:
          if: input_value
        env:
          if: step_environment
          explicit: ${{ inputs.explicit }}
"#
            .into(),
        );
        let document = Document::parse(source, DocumentKind::Workflow).unwrap();
        let expressions = document.expressions().unwrap();
        assert_eq!(
            expressions
                .iter()
                .map(|expression| expression.text.text())
                .collect::<Vec<_>>(),
            ["job_condition", "step_condition", "${{ inputs.explicit }}"]
        );
    }

    #[test]
    fn decoded_keys_and_flow_sequences_identify_conditions() {
        let source = Snapshot::new(
            "workflow.yml".into(),
            r#""\u006aobs":
  build:
    "\u0069f": 'job_condition'
    steps: [{"if": 'step_condition', with: {if: 'input_value'}}]
"#
            .into(),
        );
        let document = Document::parse(source, DocumentKind::Workflow).unwrap();
        let expressions = document.expressions().unwrap();
        assert_eq!(
            expressions
                .iter()
                .map(|expression| expression.text.text())
                .collect::<Vec<_>>(),
            ["job_condition", "step_condition"]
        );
    }

    #[test]
    fn composite_step_conditions_use_action_document_context() {
        let source = Snapshot::new("action.yml".into(), "runs:\n  using: composite\n  steps:\n    - if: step_condition\n      with:\n        if: input_value\n".into());
        let document = Document::parse(source.clone(), DocumentKind::Action).unwrap();
        let expressions = document.expressions().unwrap();
        assert_eq!(expressions.len(), 1);
        assert_eq!(expressions[0].text.text(), "step_condition");
        assert!(
            Document::parse(source, DocumentKind::Workflow)
                .unwrap()
                .expressions()
                .unwrap()
                .is_empty()
        );
    }
}
