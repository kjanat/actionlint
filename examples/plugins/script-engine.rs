//! Snippet component fixture, including partial failures and real WASI clocks.
wit_bindgen::generate!({ path: "plugins/wit", world: "script-engine-plugin" });

use exports::kjanat::actionlint::script_engine::{
    Descriptor, Diagnostic, Guest, Outcome, Position, Request, RuleDescriptor, Severity,
};

struct Plugin;

impl Guest for Plugin {
    fn describe() -> Descriptor {
        Descriptor {
            id: "echo-component".into(),
            version: "1.0.0".into(),
            name: "Echo component".into(),
            engine: "echo".into(),
            engine_version: "1.0.0".into(),
            shells: vec!["python".into()],
            rule: RuleDescriptor {
                name: "echo".into(),
                description: "Find a missing value".into(),
                category: "correctness".into(),
                subcategory: "python".into(),
                maturity: "stable".into(),
                provider: "echo-component".into(),
                recommended: true,
                explanation: String::new(),
                help: String::new(),
                documentation_url: "https://example.com/echo".into(),
                options_schema: r#"{"type":"object"}"#.into(),
                examples: Vec::new(),
            },
        }
    }

    fn validate_config(config: String) -> Result<(), String> {
        let value: serde_json::Value =
            serde_json::from_str(&config).map_err(|error| error.to_string())?;
        if !value.is_object() || value.get("invalid").is_some() {
            return Err("invalid fixture options".into());
        }
        Ok(())
    }

    fn check(request: Request) -> Outcome {
        // A real monotonic clock import must be available, without JS shims.
        let started = std::time::Instant::now();
        if request.source == "loop" {
            loop {
                std::hint::spin_loop();
            }
        }
        assert_ne!(request.source, "trap", "fixture trap");
        let mut diagnostics = Vec::new();
        let source = request.source.replace("\r\n", "\n").replace('\r', "\n");
        for (index, line) in source.lines().enumerate() {
            if let Some(offset) = line.find("missing") {
                let row = u32::try_from(index + 1).expect("fixture row fits");
                let column =
                    u32::try_from(line[..offset].chars().count() + 1).expect("fixture column fits");
                diagnostics.push(Diagnostic {
                    filename: request.filename.clone(),
                    code: "ECHO001".into(),
                    name: "missing-value".into(),
                    message: "missing value".into(),
                    severity: Severity::Warning,
                    location: Position { row, column },
                    end_location: Position {
                        row,
                        column: column + 7,
                    },
                    url: Some("https://example.com/echo".into()),
                });
            }
        }
        let options: serde_json::Value =
            serde_json::from_str(&request.config).expect("host supplies JSON");
        if options["mode"] == "invalid"
            && let Some(diagnostic) = diagnostics.last_mut()
        {
            diagnostic.end_location.column = u32::MAX;
        }
        if options["mode"] == "large-message"
            && let Some(diagnostic) = diagnostics.last_mut()
        {
            diagnostic.message = "x".repeat(8 << 20);
        }
        let _elapsed = started.elapsed();
        Outcome {
            diagnostics,
            error: (options["mode"] == "partial").then(|| "fixture analysis incomplete".into()),
        }
    }
}

export!(Plugin);
