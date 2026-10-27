use std::collections::{HashMap, HashSet};

use actionlint_model::{
    analysis::{Analysis, Completion, Diagnostic, Fact, Related, Severity},
    workflow::{Job, Name, Workflow},
};

pub fn analyze(workflow: &Workflow) -> Analysis {
    let jobs = match &workflow.jobs {
        Fact::Known(jobs) => jobs,
        Fact::Unavailable(reason) => {
            return Analysis {
                diagnostics: Vec::new(),
                completion: Completion::Incomplete(vec![*reason]),
            };
        }
    };
    let names: HashMap<String, usize> = jobs
        .iter()
        .enumerate()
        .map(|(index, job)| (job.name.value.to_lowercase(), index))
        .collect();
    let mut edges = vec![Vec::new(); jobs.len()];
    let mut diagnostics = Vec::new();
    let mut missing = Vec::new();
    for (index, job) in jobs.iter().enumerate() {
        let needs = match &job.needs {
            Fact::Known(needs) => needs,
            Fact::Unavailable(reason) => {
                if !missing.contains(reason) {
                    missing.push(*reason);
                }
                continue;
            }
        };
        let mut unique = HashSet::new();
        for dependency in needs {
            let key = dependency.value.to_lowercase();
            if !unique.insert(key.clone()) {
                continue;
            }
            if let Some(target) = names.get(&key) {
                edges[index].push((*target, dependency));
            } else {
                diagnostics.push(Diagnostic {
                    related: Vec::new(),
                    fixes: Vec::new(),
                    rule: "workflow/undefined-dependency".into(),
                    message: format!(
                        "Job {:?} depends on undefined job {:?}.",
                        job.name.value, dependency.value
                    ),
                    severity: Severity::Error,
                    primary: dependency.location.clone(),
                });
            }
        }
    }
    diagnostics.extend(cycles(jobs, &edges));
    Analysis {
        diagnostics,
        completion: if missing.is_empty() {
            Completion::Complete
        } else {
            Completion::Incomplete(missing)
        },
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Visit {
    Unseen,
    Active,
    Complete,
}

fn cycles(jobs: &[Job], edges: &[Vec<(usize, &Name)>]) -> Vec<Diagnostic> {
    let mut visits = vec![Visit::Unseen; jobs.len()];
    let mut diagnostics = Vec::new();
    for root in 0..jobs.len() {
        if visits[root] != Visit::Unseen {
            continue;
        }
        visits[root] = Visit::Active;
        let mut stack = vec![(root, 0)];
        while let Some(&(node, next)) = stack.last() {
            let Some(&(target, dependency)) = edges[node].get(next) else {
                visits[node] = Visit::Complete;
                stack.pop();
                continue;
            };
            if let Some(frame) = stack.last_mut() {
                frame.1 += 1;
            }
            match visits[target] {
                Visit::Unseen => {
                    visits[target] = Visit::Active;
                    stack.push((target, 0));
                }
                Visit::Complete => {}
                Visit::Active => {
                    let path: Vec<_> = stack
                        .iter()
                        .skip_while(|(job, _)| *job != target)
                        .map(|(job, _)| &jobs[*job])
                        .collect();
                    let names: Vec<_> = path
                        .iter()
                        .map(|job| job.name.value.as_str())
                        .chain(std::iter::once(jobs[target].name.value.as_str()))
                        .collect();
                    diagnostics.push(Diagnostic {
                        rule: "workflow/dependency-cycle".into(),
                        message: format!("Job dependency cycle: {}.", names.join(" -> ")),
                        severity: Severity::Error,
                        primary: dependency.location.clone(),
                        related: path
                            .iter()
                            .map(|job| Related {
                                message: format!(
                                    "Job {:?} participates in this cycle.",
                                    job.name.value
                                ),
                                location: job.name.location.clone(),
                            })
                            .collect(),
                        fixes: Vec::new(),
                    });
                }
            }
        }
    }
    diagnostics
}

#[cfg(test)]
mod tests {
    use super::*;
    use actionlint_model::{analysis::Unavailable, source::Snapshot};

    #[test]
    fn resolves_forward_references_and_preserves_the_missing_reference_location() {
        let source = Snapshot::new("workflow.yml".into(), "test BUILD missing build".into());
        let name = |range: std::ops::Range<usize>| {
            let location = source.span(range).unwrap();
            Name {
                value: location.text().into(),
                location,
            }
        };
        let workflow = Workflow {
            jobs: Fact::Known(vec![
                Job {
                    name: name(0..4),
                    needs: Fact::Known(vec![name(5..10), name(11..18)]),
                },
                Job {
                    name: name(19..24),
                    needs: Fact::Known(Vec::new()),
                },
            ]),
        };
        let result = analyze(&workflow);
        assert_eq!(result.completion, Completion::Complete);
        assert_eq!(result.diagnostics.len(), 1);
        assert_eq!(result.diagnostics[0].primary.text(), "missing");
        assert_eq!(result.diagnostics[0].primary.source(), &source);
    }

    #[test]
    fn missing_job_inventory_cannot_produce_a_clean_result() {
        let result = analyze(&Workflow {
            jobs: Fact::Unavailable(Unavailable::Invalid),
        });
        assert!(result.diagnostics.is_empty());
        assert_eq!(
            result.completion,
            Completion::Incomplete(vec![Unavailable::Invalid])
        );
    }

    #[test]
    fn unknown_dependencies_are_not_an_empty_list() {
        let source = Snapshot::new("workflow.yml".into(), "build".into());
        let workflow = Workflow {
            jobs: Fact::Known(vec![Job {
                name: Name {
                    value: "build".into(),
                    location: source.span(0..5).unwrap(),
                },
                needs: Fact::Unavailable(Unavailable::Unresolved),
            }]),
        };
        assert_eq!(
            analyze(&workflow).completion,
            Completion::Incomplete(vec![Unavailable::Unresolved])
        );
    }
}

#[cfg(test)]
mod cycle_tests {
    use super::*;
    use actionlint_model::{analysis::Unavailable, source::Snapshot};

    fn workflow(edges: &[(&str, &[&str])]) -> Workflow {
        let text = edges
            .iter()
            .map(|(name, _)| *name)
            .collect::<Vec<_>>()
            .join(" ");
        let source = Snapshot::new("workflow.yml".into(), text);
        let name = |value: &str| {
            let start = source.text().find(value).unwrap();
            Name {
                value: value.into(),
                location: source.span(start..start + value.len()).unwrap(),
            }
        };
        Workflow {
            jobs: Fact::Known(
                edges
                    .iter()
                    .map(|(id, needs)| Job {
                        name: name(id),
                        needs: Fact::Known(
                            needs.iter().map(|dependency| name(dependency)).collect(),
                        ),
                    })
                    .collect(),
            ),
        }
    }

    #[test]
    fn reports_cycle_witness_and_related_declarations() {
        let result = analyze(&workflow(&[("a", &["b"]), ("b", &["c"]), ("c", &["a"])]));
        assert_eq!(result.diagnostics.len(), 1);
        let diagnostic = &result.diagnostics[0];
        assert_eq!(diagnostic.rule, "workflow/dependency-cycle");
        assert_eq!(diagnostic.primary.text(), "a");
        assert_eq!(diagnostic.related.len(), 3);
        assert!(diagnostic.message.contains("a -> b -> c -> a"));
    }

    #[test]
    fn shared_dependencies_are_not_cycles() {
        let result = analyze(&workflow(&[
            ("a", &["b", "c"]),
            ("b", &["d"]),
            ("c", &["d"]),
            ("d", &[]),
        ]));
        assert!(result.diagnostics.is_empty());
        assert_eq!(result.completion, Completion::Complete);
    }

    #[test]
    fn self_cycle_and_unknown_edges_can_coexist() {
        let mut workflow = workflow(&[("a", &["a"]), ("b", &[])]);
        let Fact::Known(jobs) = &mut workflow.jobs else {
            unreachable!()
        };
        jobs[1].needs = Fact::Unavailable(Unavailable::Unresolved);
        let result = analyze(&workflow);
        assert_eq!(result.diagnostics.len(), 1);
        assert_eq!(
            result.completion,
            Completion::Incomplete(vec![Unavailable::Unresolved])
        );
    }
}
