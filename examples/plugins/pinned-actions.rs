//! Dependency-fact equivalent of the native pinnedactions example. Source text
//! is intentionally unused: the host owns parsing and declaration provenance.
wit_bindgen::generate!({
    path: "crates/actionlint-extension-api/wit",
    world: "plugin",
});

use exports::kjanat::actionlint::analyzer::{Descriptor, Diagnostic, Fact, Guest, Input, Outcome};

struct Plugin;

impl Guest for Plugin {
    fn describe() -> Descriptor {
        Descriptor {
            name: "pinned-actions".into(),
            description: "Pin remote dependencies to full commit hashes".into(),
            requires: vec![Fact::Dependencies],
            category: "security".into(),
            subcategory: "dependencies".into(),
            maturity: "experimental".into(),
            provider: "example".into(),
            recommended: false,
            explanation: String::new(),
            help: String::new(),
            documentation_url: String::new(),
            fix_availability: "none".into(),
            options_schema: "{}".into(),
            examples: Vec::new(),
        }
    }

    fn analyze(input: Input) -> Outcome {
        let mut diagnostics = Vec::new();
        for dependency in input.dependencies {
            let kind = match dependency.kind.as_str() {
                "action" => "action",
                "workflow" => "reusable workflow",
                _ => continue,
            };
            if dependency.uses.starts_with("./") || dependency.uses.starts_with("docker://") {
                continue;
            }
            // Malformed references belong to syntax validation.
            let Some((_, reference)) = dependency.uses.rsplit_once('@') else {
                continue;
            };
            if reference.len() == 40 && reference.bytes().all(|c| c.is_ascii_hexdigit()) {
                continue;
            }
            diagnostics.push(Diagnostic {
                reference: Some(dependency.reference),
                rule: "pinned-actions".into(),
                code: "unpinned-dependency".into(),
                severity: "warning".into(),
                message: format!("{kind} must be pinned to a full commit hash"),
                path: input.path.clone(),
                start: dependency.start,
                end: dependency.end,
                explanation: String::new(),
                help: String::new(),
                documentation_url: String::new(),
                related: Vec::new(),
                fixes: Vec::new(),
            });
        }
        Outcome {
            diagnostics,
            error: None,
        }
    }
}

#[cfg(target_arch = "wasm32")]
export!(Plugin);
