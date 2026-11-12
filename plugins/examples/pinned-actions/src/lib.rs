//! Checks dependency facts supplied by actionlint. actionlint parses the source
//! and supplies declaration provenance, so this plugin does not read source text.
use actionlint_extension_api::{Descriptor, Diagnostic, Guest, Input, InputKind, Outcome};

pub struct Plugin;

impl Guest for Plugin {
    fn describe() -> Descriptor {
        Descriptor {
            name: "pinned-actions".into(),
            description: "Pin remote dependencies to full commit hashes".into(),
            required_inputs: vec![InputKind::Dependencies],
            category: "security".into(),
            subcategory: "dependencies".into(),
            maturity: String::new(),
            provider: "example".into(),
            recommended: false,
            explanation: String::new(),
            help: String::new(),
            documentation_url: String::new(),
            fix_availability: String::new(), // Legacy ABI field; actionlint ignores it.
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
actionlint_extension_api::export_plugin!(Plugin);
