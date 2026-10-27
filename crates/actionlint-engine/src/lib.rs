//! Analysis orchestration over snapshots and injected execution drivers.

use actionlint_model::{
    analysis::{Analysis, Completion, Fact, Input, Unavailable},
    source::Snapshot,
};
use actionlint_syntax::{Document, DocumentKind};

pub trait WorkflowAnalyzer {
    fn analyze(&self, input: &Input<'_>) -> Result<Analysis, String>;
}

#[derive(Debug)]
pub enum Failure {
    Parse(String),
    Analyzer { name: String, message: String },
}

pub struct Run {
    pub analysis: Analysis,
    pub failures: Vec<Failure>,
}

pub struct Analyzer<'a> {
    pub name: &'a str,
    pub implementation: &'a dyn WorkflowAnalyzer,
}

pub fn analyze_workflow(source: Snapshot, analyzers: &[Analyzer<'_>]) -> Run {
    let lowered = Document::parse(source.clone(), DocumentKind::Workflow)
        .and_then(|document| Ok((document.workflow()?, document.expressions()?)));
    let (lowered, expressions) = match lowered {
        Ok(lowered) => lowered,
        Err(error) => {
            return Run {
                analysis: Analysis {
                    diagnostics: Vec::new(),
                    completion: Completion::Incomplete(vec![Unavailable::Invalid]),
                },
                failures: vec![Failure::Parse(error.to_string())],
            };
        }
    };
    let mut missing = Vec::new();
    match &lowered.workflow.jobs {
        Fact::Unavailable(reason) => missing.push(*reason),
        Fact::Known(jobs) => {
            for job in jobs {
                if let Fact::Unavailable(reason) = job.needs
                    && !missing.contains(&reason)
                {
                    missing.push(reason);
                }
            }
        }
    }
    if !lowered.diagnostics.is_empty() && !missing.contains(&Unavailable::Invalid) {
        missing.push(Unavailable::Invalid);
    }
    let mut diagnostics = lowered.diagnostics;
    let mut failures = Vec::new();
    if analyzers.is_empty() {
        missing.push(Unavailable::Unsupported);
    }
    for analyzer in analyzers {
        match analyzer.implementation.analyze(&Input {
            source: &source,
            workflow: &lowered.workflow,
            expressions: &expressions,
        }) {
            Ok(mut analysis) => {
                if let Completion::Incomplete(reasons) = analysis.completion {
                    if reasons.is_empty() {
                        failures.push(Failure::Analyzer {
                            name: analyzer.name.into(),
                            message: "incomplete analysis has no reason".into(),
                        });
                        if !missing.contains(&Unavailable::Invalid) {
                            missing.push(Unavailable::Invalid);
                        }
                    }
                    for reason in reasons {
                        if !missing.contains(&reason) {
                            missing.push(reason);
                        }
                    }
                }
                for diagnostic in analysis.diagnostics.drain(..) {
                    match diagnostic.validate(std::slice::from_ref(&source)) {
                        Ok(()) => diagnostics.push(diagnostic),
                        Err(error) => {
                            failures.push(Failure::Analyzer {
                                name: analyzer.name.into(),
                                message: error.to_string(),
                            });
                            if !missing.contains(&Unavailable::Invalid) {
                                missing.push(Unavailable::Invalid);
                            }
                        }
                    }
                }
            }
            Err(message) => {
                failures.push(Failure::Analyzer {
                    name: analyzer.name.into(),
                    message,
                });
                if !missing.contains(&Unavailable::Unresolved) {
                    missing.push(Unavailable::Unresolved);
                }
            }
        }
    }
    Run {
        analysis: Analysis {
            diagnostics,
            completion: if missing.is_empty() {
                Completion::Complete
            } else {
                Completion::Incomplete(missing)
            },
        },
        failures,
    }
}
