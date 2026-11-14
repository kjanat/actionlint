//! Runner support transport fixture. Snapshot content selects malformed replies
//! so the host can exercise admission after an actual component call.
wit_bindgen::generate!({
    path: "../../../../../../../plugins/protocol/wit/deps/actionlint-0.3.0",
    world: "runner-plugin",
});

use exports::kjanat::actionlint::runner_provider::{
    Answer, Candidate, CaseSensitivity, DatasetStatus, Descriptor, Freshness, Guest, Hosting, Os,
    Outcome, Request, Resolution, ServiceScope, SupportCatalog, SupportChange, SupportPolicy,
    SupportState,
};

struct Plugin;

impl Guest for Plugin {
    fn describe() -> Descriptor {
        Descriptor {
            name: "runner-support-fixture".into(),
            version: "1".into(),
            targets: vec!["github".into()],
        }
    }

    fn resolve(request: Request) -> Outcome {
        if request.snapshot.content == "error" {
            return Outcome {
                answers: vec![],
                support: None,
                error: Some("fixture acquisition failed".into()),
            };
        }
        let status = DatasetStatus {
            dataset_id: "runner-support-fixture:v1".into(),
            scope: request.scope.clone(),
            obtained_at: "2026-11-12T00:00:00Z".into(),
            freshness: Freshness::Fresh,
            complete: true,
        };
        let answers = request
            .selectors
            .iter()
            .map(|_| Answer {
                resolution: Resolution::Resolved(vec![Candidate {
                    name: format!(
                        "{}:{}:{}",
                        request.service.kind,
                        request.service.version.as_deref().unwrap_or(""),
                        request.as_of
                    ),
                    os: Os::Linux,
                    architecture: "arm32".into(),
                    hosting: Hosting::SelfHosted,
                    default_shell: "bash".into(),
                    case_sensitivity: CaseSensitivity::Sensitive,
                    sh_is_dash: false,
                    ephemeral: false,
                }]),
                dataset_status: status.clone(),
            })
            .collect();
        let mut policy = SupportPolicy {
            id: "arm32-fixture".into(),
            description: "ARM32 support retirement".into(),
            url: "https://example.com/runner-support".into(),
            capability: "job-execution".into(),
            service: ServiceScope {
                kind: request.service.kind.clone(),
                min_version: (request.service.kind == "ghes").then(|| "3.18".into()),
                before_version: (request.service.kind == "ghes").then(|| "3.20".into()),
            },
            hosting: vec![Hosting::SelfHosted],
            os: vec![Os::Linux, Os::Windows],
            architectures: vec!["arm32".into()],
            initial: SupportState::Unknown,
            changes: vec![
                SupportChange {
                    on: "2026-11-12".into(),
                    state: SupportState::Deprecated,
                },
                SupportChange {
                    on: "2026-12-08".into(),
                    state: SupportState::Unsupported,
                },
            ],
        };
        match request.snapshot.content.as_str() {
            "bad-date" => policy.changes[0].on = "2026-02-30".into(),
            "unordered" => policy.changes.reverse(),
            "empty-id" => policy.id.clear(),
            "unknown-service" => policy.service.kind = "made-up-service".into(),
            "bad-url" => policy.url = "javascript:alert(1)".into(),
            "bad-version" => policy.service.min_version = Some("not-a-version".into()),
            _ => {}
        }
        let policies = if request.snapshot.content == "duplicate" {
            vec![policy.clone(), policy]
        } else {
            vec![policy]
        };
        let mut support = SupportCatalog {
            dataset_status: status,
            policies,
        };
        if request.snapshot.content == "foreign-scope" {
            support.dataset_status.scope = "other/repo".into();
        }
        Outcome {
            answers,
            support: (request.snapshot.content != "missing").then_some(support),
            error: None,
        }
    }
}

export!(Plugin);
