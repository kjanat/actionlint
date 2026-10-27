use actionlint_engine::WorkflowAnalyzer;
use actionlint_model::analysis as model;

use crate::{CompiledComponent, bindings::actionlint::extension::types as wire};

fn lower_reason(value: model::Unavailable) -> wire::Unavailable {
    match value {
        model::Unavailable::Unresolved => wire::Unavailable::Unresolved,
        model::Unavailable::Invalid => wire::Unavailable::Invalid,
        model::Unavailable::Unsupported => wire::Unavailable::Unsupported,
        model::Unavailable::Cancelled => wire::Unavailable::Cancelled,
        model::Unavailable::BudgetExceeded => wire::Unavailable::BudgetExceeded,
    }
}

fn lift_reason(value: wire::Unavailable) -> model::Unavailable {
    match value {
        wire::Unavailable::Unresolved => model::Unavailable::Unresolved,
        wire::Unavailable::Invalid => model::Unavailable::Invalid,
        wire::Unavailable::Unsupported => model::Unavailable::Unsupported,
        wire::Unavailable::Cancelled => model::Unavailable::Cancelled,
        wire::Unavailable::BudgetExceeded => model::Unavailable::BudgetExceeded,
    }
}

impl WorkflowAnalyzer for CompiledComponent {
    fn analyze(&self, input: &model::Input<'_>) -> Result<model::Analysis, String> {
        let source = input.source;
        let workflow = input.workflow;
        let name = |name: &actionlint_model::workflow::Name| -> Result<wire::Name, String> {
            if name.location.source() != source {
                return Err("workflow fact belongs to another source revision".into());
            }
            let range = name.location.bytes();
            Ok(wire::Name {
                value: name.value.clone(),
                location: wire::Range {
                    source: 0,
                    start: u32::try_from(range.start).map_err(|e| e.to_string())?,
                    end: u32::try_from(range.end).map_err(|e| e.to_string())?,
                },
            })
        };
        let jobs = match &workflow.jobs {
            model::Fact::Unavailable(reason) => wire::Jobs::Unavailable(lower_reason(*reason)),
            model::Fact::Known(jobs) => wire::Jobs::Known(
                jobs.iter()
                    .map(|job| {
                        let needs = match &job.needs {
                            model::Fact::Unavailable(reason) => {
                                wire::Dependencies::Unavailable(lower_reason(*reason))
                            }
                            model::Fact::Known(names) => wire::Dependencies::Known(
                                names.iter().map(&name).collect::<Result<Vec<_>, _>>()?,
                            ),
                        };
                        Ok(wire::Job {
                            name: name(&job.name)?,
                            needs,
                        })
                    })
                    .collect::<Result<Vec<_>, String>>()?,
            ),
        };
        // The component instance and its source IDs live for exactly this call.
        let request = wire::Request {
            snapshot: 0,
            sources: vec![wire::Source {
                id: 0,
                text: source.text().into(),
            }],
            jobs,
            expressions: input
                .expressions
                .iter()
                .map(|expression| {
                    use actionlint_model::{expression::ExpressionKind, source::Origin};
                    let range =
                        |span: &actionlint_model::source::Span| -> Result<wire::Range, String> {
                            if span.source() != source {
                                return Err("expression belongs to another source revision".into());
                            }
                            Ok(wire::Range {
                                source: 0,
                                start: u32::try_from(span.bytes().start)
                                    .map_err(|e| e.to_string())?,
                                end: u32::try_from(span.bytes().end).map_err(|e| e.to_string())?,
                            })
                        };
                    let text = expression
                        .text
                        .segments()
                        .map(|(text, origin)| {
                            let origin = match origin {
                                Origin::Exact(span) => wire::Origin::Exact(range(span)?),
                                Origin::Transformed(span) => {
                                    wire::Origin::Transformed(range(span)?)
                                }
                                Origin::Generated => wire::Origin::Generated,
                            };
                            Ok(wire::TextSegment {
                                text: text.into(),
                                origin,
                            })
                        })
                        .collect::<Result<Vec<_>, String>>()?;
                    Ok(wire::Expression {
                        kind: match expression.kind {
                            ExpressionKind::Delimited => wire::ExpressionKind::Delimited,
                            ExpressionKind::BareCondition => wire::ExpressionKind::BareCondition,
                            ExpressionKind::Unterminated => wire::ExpressionKind::Unterminated,
                        },
                        location: range(&expression.location)?,
                        text,
                    })
                })
                .collect::<Result<Vec<_>, String>>()?,
        };
        let result = self
            .analyze(&request)
            .map_err(|e| format!("{e:#}"))?
            .map_err(|failure| match failure {
                wire::Failure::InvalidInput(message) => {
                    format!("component rejected input: {message}")
                }
                wire::Failure::Unsupported(message) => {
                    format!("component cannot analyze input: {message}")
                }
                wire::Failure::Failed(message) => format!("component analysis failed: {message}"),
            })?;
        let diagnostics = result
            .diagnostics
            .into_iter()
            .map(|diagnostic| {
                let span = |range: wire::Range| {
                    let start = usize::try_from(range.start).map_err(|e| e.to_string())?;
                    let end = usize::try_from(range.end).map_err(|e| e.to_string())?;
                    source.span(start..end).map_err(|e| e.to_string())
                };
                Ok(model::Diagnostic {
                    rule: diagnostic.rule,
                    message: diagnostic.message,
                    severity: match diagnostic.severity {
                        wire::Severity::Error => model::Severity::Error,
                        wire::Severity::Warning => model::Severity::Warning,
                        wire::Severity::Information => model::Severity::Information,
                        wire::Severity::Hint => model::Severity::Hint,
                    },
                    primary: span(diagnostic.primary)?,
                    related: diagnostic
                        .related
                        .into_iter()
                        .map(|related| {
                            Ok(model::Related {
                                message: related.message,
                                location: span(related.location)?,
                            })
                        })
                        .collect::<Result<Vec<_>, String>>()?,
                    fixes: diagnostic
                        .fixes
                        .into_iter()
                        .map(|fix| {
                            Ok(model::Fix {
                                title: fix.title,
                                applicability: match fix.applicability {
                                    wire::Applicability::Automatic => {
                                        model::Applicability::Automatic
                                    }
                                    wire::Applicability::RequiresReview => {
                                        model::Applicability::RequiresReview
                                    }
                                },
                                edits: fix
                                    .edits
                                    .into_iter()
                                    .map(|edit| {
                                        Ok(actionlint_model::source::Edit {
                                            span: span(edit.location)?,
                                            replacement: edit.replacement,
                                        })
                                    })
                                    .collect::<Result<Vec<_>, String>>()?,
                            })
                        })
                        .collect::<Result<Vec<_>, String>>()?,
                })
            })
            .collect::<Result<Vec<_>, String>>()?;
        Ok(model::Analysis {
            diagnostics,
            completion: match result.completion {
                wire::Completion::Complete => model::Completion::Complete,
                wire::Completion::Incomplete(reasons) => {
                    model::Completion::Incomplete(reasons.into_iter().map(lift_reason).collect())
                }
            },
        })
    }
}
