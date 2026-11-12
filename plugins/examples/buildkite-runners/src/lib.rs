//! Offline Buildkite queue mapping example. A supplied snapshot describes queue
//! labels and explicit platform guarantees; it never implies GitHub image parity.
wit_bindgen::generate!({ path: "../../protocol/wit", world: "runner-plugin" });

use exports::kjanat::actionlint0_2_0::runner_provider::{
    Answer, Candidate, CaseSensitivity, DatasetStatus, Descriptor, Freshness, Guest, Os, Outcome,
    Position, Rejection, Request, Resolution, Selector, Term,
};
use serde::Deserialize;
use sha2::{Digest, Sha256};

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Catalogue {
    queues: Vec<Queue>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Queue {
    name: String,
    labels: Vec<String>,
    group: Option<String>,
    os: Platform,
    architecture: String,
    shell: String,
}

#[derive(Deserialize)]
#[serde(rename_all = "lowercase")]
enum Platform {
    Linux,
    Macos,
    Windows,
}

pub struct Plugin;

impl Guest for Plugin {
    fn describe() -> Descriptor {
        Descriptor {
            name: "buildkite-runners".into(),
            version: env!("CARGO_PKG_VERSION").into(),
            targets: vec!["buildkite".into()],
        }
    }

    fn resolve(request: Request) -> Outcome {
        let catalogue = if request.snapshot.content.is_empty() {
            None
        } else {
            match serde_json::from_str::<Catalogue>(&request.snapshot.content) {
                Ok(catalogue) => Some(catalogue),
                Err(error) => {
                    return Outcome {
                        answers: Vec::new(),
                        error: Some(format!("invalid queue snapshot: {error}")),
                    };
                }
            }
        };
        let dataset = format!(
            "buildkite-queue-snapshot/v1:{:x}",
            Sha256::digest(request.snapshot.content.as_bytes())
        );
        let snapshot_in_scope = request.snapshot.scope == request.scope;
        let answers = request
            .selectors
            .iter()
            .map(|selector| Answer {
                resolution: resolve_selector(&request, selector, catalogue.as_ref()),
                dataset_status: DatasetStatus {
                    dataset_id: dataset.clone(),
                    scope: request.scope.clone(),
                    obtained_at: request.snapshot.obtained_at.clone(),
                    freshness: if catalogue.is_some() && snapshot_in_scope {
                        request.snapshot.freshness
                    } else {
                        Freshness::Missing
                    },
                    complete: catalogue.is_some() && snapshot_in_scope && request.snapshot.complete,
                },
            })
            .collect();
        Outcome {
            answers,
            error: None,
        }
    }
}

fn resolve_selector(
    request: &Request,
    selector: &Selector,
    catalogue: Option<&Catalogue>,
) -> Resolution {
    if request.target != "buildkite" {
        return Resolution::NotApplicable;
    }
    if request.snapshot.scope != request.scope {
        return Resolution::Unknown("snapshot belongs to a different scope".into());
    }
    let Some(catalogue) = catalogue else {
        return Resolution::Unknown("queue snapshot is missing".into());
    };
    if !matches!(request.snapshot.freshness, Freshness::Fresh) {
        return Resolution::Unknown("queue evidence is stale or missing".into());
    }
    if !request.snapshot.complete {
        return Resolution::Unknown("queue snapshot is incomplete".into());
    }
    if selector.incomplete
        || !selector.expression.is_empty()
        || selector
            .labels
            .iter()
            .any(|label| matches!(label.value, Term::Expression(_)))
        || selector
            .group
            .as_ref()
            .is_some_and(|group| matches!(group.value, Term::Expression(_)))
    {
        return Resolution::Unknown("selector depends on runtime values".into());
    }
    let candidates: Vec<Candidate> = catalogue
        .queues
        .iter()
        .filter(|queue| {
            let group_matches = match selector.group.as_ref().map(|group| &group.value) {
                None => true,
                Some(Term::Literal(group)) => queue.group.as_ref() == Some(group),
                Some(Term::Expression(_)) => false,
            };
            group_matches
                && selector.labels.iter().all(|label| match &label.value {
                    Term::Literal(label) => queue.labels.iter().any(|known| known == label),
                    Term::Expression(_) => false,
                })
        })
        .map(|queue| Candidate {
            name: queue.name.clone(),
            os: match queue.os {
                Platform::Linux => Os::Linux,
                Platform::Macos => Os::Macos,
                Platform::Windows => Os::Windows,
            },
            architecture: queue.architecture.clone(),
            default_shell: queue.shell.clone(),
            case_sensitivity: match queue.os {
                Platform::Windows => CaseSensitivity::Insensitive,
                Platform::Linux => CaseSensitivity::Sensitive,
                Platform::Macos => CaseSensitivity::Unknown,
            },
            sh_is_dash: false,
            ephemeral: false,
        })
        .collect();
    if !candidates.is_empty() {
        return Resolution::Resolved(candidates);
    }
    let position = selector
        .labels
        .first()
        .map(|label| label.position)
        .unwrap_or(Position { line: 0, column: 0 });
    Resolution::NoMatch(vec![Rejection {
        reason: "no queue matches all runner labels and group in this snapshot".into(),
        position,
    }])
}

#[cfg(target_arch = "wasm32")]
export!(Plugin);

#[cfg(test)]
mod tests {
    use super::*;
    use exports::kjanat::actionlint0_2_0::runner_provider::{Constraint, Snapshot};

    const QUEUES: &str = r#"{"queues":[
        {"name":"linux-build","labels":["build","linux"],"group":"ci","os":"linux","architecture":"x64","shell":"bash"},
        {"name":"windows-build","labels":["build","windows"],"group":"ci","os":"windows","architecture":"arm64","shell":"pwsh"},
        {"name":"mac-release","labels":["release","macos"],"group":"release","os":"macos","architecture":"arm64","shell":"bash"}
    ]}"#;

    fn request(labels: &[&str]) -> Request {
        Request {
            target: "buildkite".into(),
            scope: "org/repo".into(),
            snapshot: Snapshot {
                content: QUEUES.into(),
                scope: "org/repo".into(),
                obtained_at: "2026-11-06T00:00:00Z".into(),
                freshness: Freshness::Fresh,
                complete: true,
            },
            selectors: vec![Selector {
                incomplete: false,
                expression: String::new(),
                labels: labels
                    .iter()
                    .map(|label| Constraint {
                        value: Term::Literal((*label).into()),
                        position: Position {
                            line: 4,
                            column: 15,
                        },
                    })
                    .collect(),
                group: None,
            }],
        }
    }

    #[test]
    fn resolves_queue_platforms_and_snapshot_identity() {
        for (label, name, os, architecture, shell, sensitivity) in [
            (
                "linux",
                "linux-build",
                Os::Linux,
                "x64",
                "bash",
                CaseSensitivity::Sensitive,
            ),
            (
                "windows",
                "windows-build",
                Os::Windows,
                "arm64",
                "pwsh",
                CaseSensitivity::Insensitive,
            ),
            (
                "macos",
                "mac-release",
                Os::Macos,
                "arm64",
                "bash",
                CaseSensitivity::Unknown,
            ),
        ] {
            let outcome = Plugin::resolve(request(&[label]));
            assert!(outcome.error.is_none());
            assert_eq!(outcome.answers.len(), 1);
            let answer = &outcome.answers[0];
            let Resolution::Resolved(candidates) = &answer.resolution else {
                panic!("expected a resolved queue: {:?}", answer.resolution);
            };
            assert_eq!(candidates.len(), 1);
            let candidate = &candidates[0];
            assert_eq!(candidate.name, name);
            assert_eq!(candidate.os, os);
            assert_eq!(candidate.architecture, architecture);
            assert_eq!(candidate.default_shell, shell);
            assert_eq!(candidate.case_sensitivity, sensitivity);
            assert!(!candidate.ephemeral);
            assert!(!candidate.sh_is_dash);
            assert!(answer.dataset_status.complete);
            assert_eq!(answer.dataset_status.scope, "org/repo");
            assert_eq!(answer.dataset_status.obtained_at, "2026-11-06T00:00:00Z");
            assert_eq!(answer.dataset_status.freshness, Freshness::Fresh);
            assert_eq!(
                answer.dataset_status.dataset_id,
                format!(
                    "buildkite-queue-snapshot/v1:{:x}",
                    Sha256::digest(QUEUES.as_bytes())
                )
            );
        }
    }

    #[test]
    fn labels_and_group_must_match_one_queue() {
        for labels in [&["linux", "windows"][..], &["build", "macos"][..]] {
            let outcome = Plugin::resolve(request(labels));
            let Resolution::NoMatch(rejections) = &outcome.answers[0].resolution else {
                panic!("combined labels from different queues");
            };
            assert_eq!(rejections.len(), 1);
            assert_eq!(
                (rejections[0].position.line, rejections[0].position.column),
                (4, 15)
            );
        }
        let mut input = request(&["linux"]);
        input.selectors[0].group = Some(Constraint {
            value: Term::Literal("release".into()),
            position: Position {
                line: 3,
                column: 10,
            },
        });
        assert!(matches!(
            Plugin::resolve(input).answers[0].resolution,
            Resolution::NoMatch(_)
        ));
    }

    #[test]
    fn unavailable_or_incomplete_snapshots_do_not_prove_absence() {
        for state in ["missing", "stale", "incomplete", "foreign scope"] {
            let mut input = request(&["does-not-exist"]);
            match state {
                "missing" => input.snapshot.content.clear(),
                "stale" => input.snapshot.freshness = Freshness::Stale,
                "incomplete" => input.snapshot.complete = false,
                "foreign scope" => input.snapshot.scope = "other/repo".into(),
                _ => unreachable!(),
            }
            let outcome = Plugin::resolve(input);
            assert!(outcome.error.is_none());
            assert!(
                matches!(outcome.answers[0].resolution, Resolution::Unknown(_)),
                "{state}"
            );
            if state == "missing" || state == "foreign scope" {
                assert_eq!(
                    outcome.answers[0].dataset_status.freshness,
                    Freshness::Missing
                );
                assert!(!outcome.answers[0].dataset_status.complete);
            }
        }
    }

    #[test]
    fn runtime_selectors_do_not_become_literal_labels() {
        for state in ["label", "group", "expression", "incomplete"] {
            let mut input = request(&["linux"]);
            match state {
                "label" => {
                    input.selectors[0].labels[0].value = Term::Expression("inputs.queue".into())
                }
                "group" => {
                    input.selectors[0].group = Some(Constraint {
                        value: Term::Expression("inputs.group".into()),
                        position: Position {
                            line: 3,
                            column: 10,
                        },
                    })
                }
                "expression" => input.selectors[0].expression = "inputs.runners".into(),
                "incomplete" => input.selectors[0].incomplete = true,
                _ => unreachable!(),
            }
            assert!(
                matches!(
                    Plugin::resolve(input).answers[0].resolution,
                    Resolution::Unknown(_)
                ),
                "{state}"
            );
        }
    }

    #[test]
    fn selector_order_and_other_targets_are_explicit() {
        let mut input = request(&["linux"]);
        input
            .selectors
            .push(request(&["windows"]).selectors.remove(0));
        let outcome = Plugin::resolve(input);
        assert_eq!(outcome.answers.len(), 2);
        for (answer, name) in outcome.answers.iter().zip(["linux-build", "windows-build"]) {
            let Resolution::Resolved(candidates) = &answer.resolution else {
                panic!("lost selector outcome")
            };
            assert_eq!(candidates[0].name, name);
        }
        let mut input = request(&["linux"]);
        input.target = "github".into();
        assert!(matches!(
            Plugin::resolve(input).answers[0].resolution,
            Resolution::NotApplicable
        ));
    }

    #[test]
    fn malformed_catalogues_fail_the_whole_batch() {
        for content in [
            "{",
            r#"{"queues":[],"ignored":true}"#,
            r#"{"queues":[{"name":"bad","labels":[],"os":"plan9","architecture":"x64","shell":"sh"}]}"#,
        ] {
            let mut input = request(&["linux"]);
            input.snapshot.content = content.into();
            let outcome = Plugin::resolve(input);
            assert!(outcome.error.is_some());
            assert!(outcome.answers.is_empty());
        }
    }
}
