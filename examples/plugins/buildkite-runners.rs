//! Offline Buildkite queue mapping example. A supplied snapshot describes queue
//! labels and explicit platform guarantees; it never implies GitHub image parity.
wit_bindgen::generate!({ path: "crates/actionlint-extension-api/wit", world: "runner-plugin" });

use exports::kjanat::actionlint::runner_provider::{
    Answer, Candidate, CaseSensitivity, Descriptor, Evidence, Freshness, Guest, Os, Outcome,
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

struct Plugin;

impl Guest for Plugin {
    fn describe() -> Descriptor {
        Descriptor {
            name: "buildkite-runners".into(),
            version: "1.0.0".into(),
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
        let answers = request
            .selectors
            .iter()
            .map(|selector| Answer {
                resolution: resolve_selector(&request, selector, catalogue.as_ref()),
                evidence: Evidence {
                    dataset: dataset.clone(),
                    scope: request.scope.clone(),
                    obtained_at: request.snapshot.obtained_at.clone(),
                    freshness: if catalogue.is_some() {
                        request.snapshot.freshness
                    } else {
                        Freshness::Missing
                    },
                    complete: catalogue.is_some() && request.snapshot.complete,
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

export!(Plugin);
