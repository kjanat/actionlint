use actionlint_model::{analysis as model, source::Snapshot, workflow};
use actionlint_plugin_sdk::{
    actionlint::extension::types as wire, exports::actionlint::extension::analysis::Guest,
};

struct WorkflowRules;

fn unavailable(value: wire::Unavailable) -> model::Unavailable {
    match value {
        wire::Unavailable::Unresolved => model::Unavailable::Unresolved,
        wire::Unavailable::Invalid => model::Unavailable::Invalid,
        wire::Unavailable::Unsupported => model::Unavailable::Unsupported,
        wire::Unavailable::Cancelled => model::Unavailable::Cancelled,
        wire::Unavailable::BudgetExceeded => model::Unavailable::BudgetExceeded,
    }
}

fn lower_unavailable(value: model::Unavailable) -> wire::Unavailable {
    match value {
        model::Unavailable::Unresolved => wire::Unavailable::Unresolved,
        model::Unavailable::Invalid => wire::Unavailable::Invalid,
        model::Unavailable::Unsupported => wire::Unavailable::Unsupported,
        model::Unavailable::Cancelled => wire::Unavailable::Cancelled,
        model::Unavailable::BudgetExceeded => wire::Unavailable::BudgetExceeded,
    }
}

impl Guest for WorkflowRules {
    fn analyze(input: wire::Request) -> Result<wire::AnalysisResult, wire::Failure> {
        let mut sources = std::collections::BTreeMap::new();
        for source in input.sources {
            if sources
                .insert(source.id, Snapshot::new(source.id.to_string(), source.text))
                .is_some()
            {
                return Err(wire::Failure::InvalidInput("duplicate source ID".into()));
            }
        }
        let name = |name: wire::Name| -> Result<workflow::Name, wire::Failure> {
            let source = sources
                .get(&name.location.source)
                .ok_or_else(|| wire::Failure::InvalidInput("unknown source ID".into()))?;
            let start = usize::try_from(name.location.start)
                .map_err(|error| wire::Failure::InvalidInput(error.to_string()))?;
            let end = usize::try_from(name.location.end)
                .map_err(|error| wire::Failure::InvalidInput(error.to_string()))?;
            let location = source
                .span(start..end)
                .map_err(|error| wire::Failure::InvalidInput(error.to_string()))?;
            Ok(workflow::Name {
                value: name.value,
                location,
            })
        };
        let jobs = match input.jobs {
            wire::Jobs::Unavailable(reason) => model::Fact::Unavailable(unavailable(reason)),
            wire::Jobs::Known(jobs) => model::Fact::Known(
                jobs.into_iter()
                    .map(|job| {
                        let needs = match job.needs {
                            wire::Dependencies::Unavailable(reason) => {
                                model::Fact::Unavailable(unavailable(reason))
                            }
                            wire::Dependencies::Known(names) => model::Fact::Known(
                                names
                                    .into_iter()
                                    .map(&name)
                                    .collect::<Result<Vec<_>, _>>()?,
                            ),
                        };
                        Ok(workflow::Job {
                            name: name(job.name)?,
                            needs,
                        })
                    })
                    .collect::<Result<Vec<_>, wire::Failure>>()?,
            ),
        };
        let result =
            actionlint_rules::workflow::dependencies::analyze(&workflow::Workflow { jobs });
        let diagnostics = result
            .diagnostics
            .into_iter()
            .map(|diagnostic| {
                Ok(wire::Diagnostic {
                    rule: diagnostic.rule,
                    message: diagnostic.message,
                    severity: match diagnostic.severity {
                        model::Severity::Error => wire::Severity::Error,
                        model::Severity::Warning => wire::Severity::Warning,
                        model::Severity::Information => wire::Severity::Information,
                        model::Severity::Hint => wire::Severity::Hint,
                    },
                    primary: lower_range(&sources, &diagnostic.primary)?,
                    related: diagnostic
                        .related
                        .into_iter()
                        .map(|related| {
                            Ok(wire::Related {
                                message: related.message,
                                location: lower_range(&sources, &related.location)?,
                            })
                        })
                        .collect::<Result<Vec<_>, wire::Failure>>()?,
                    fixes: diagnostic
                        .fixes
                        .into_iter()
                        .map(|fix| {
                            Ok(wire::Fix {
                                title: fix.title,
                                applicability: match fix.applicability {
                                    model::Applicability::Automatic => {
                                        wire::Applicability::Automatic
                                    }
                                    model::Applicability::RequiresReview => {
                                        wire::Applicability::RequiresReview
                                    }
                                },
                                edits: fix
                                    .edits
                                    .into_iter()
                                    .map(|edit| {
                                        Ok(wire::Edit {
                                            location: lower_range(&sources, &edit.span)?,
                                            replacement: edit.replacement,
                                        })
                                    })
                                    .collect::<Result<Vec<_>, wire::Failure>>()?,
                            })
                        })
                        .collect::<Result<Vec<_>, wire::Failure>>()?,
                })
            })
            .collect::<Result<Vec<_>, wire::Failure>>()?;
        Ok(wire::AnalysisResult {
            snapshot: input.snapshot,
            diagnostics,
            completion: match result.completion {
                model::Completion::Complete => wire::Completion::Complete,
                model::Completion::Incomplete(reasons) => wire::Completion::Incomplete(
                    reasons.into_iter().map(lower_unavailable).collect(),
                ),
            },
        })
    }
}

actionlint_plugin_sdk::export!(WorkflowRules with_types_in actionlint_plugin_sdk);

fn lower_range(
    sources: &std::collections::BTreeMap<u64, Snapshot>,
    span: &actionlint_model::source::Span,
) -> Result<wire::Range, wire::Failure> {
    let source = sources
        .iter()
        .find(|(_, snapshot)| *snapshot == span.source())
        .map(|(id, _)| *id)
        .ok_or_else(|| wire::Failure::Failed("diagnostic source missing".into()))?;
    let range = span.bytes();
    Ok(wire::Range {
        source,
        start: u32::try_from(range.start)
            .map_err(|error| wire::Failure::Failed(error.to_string()))?,
        end: u32::try_from(range.end).map_err(|error| wire::Failure::Failed(error.to_string()))?,
    })
}
