wit_bindgen::generate!({
    path: "../../../plugins/wit",
    world: "plugin",
});

use exports::kjanat::actionlint::analyzer::{
    Descriptor, Diagnostic, Fact, Guest, Input, Outcome, Position,
};
use kjanat::actionlint::host::log;

struct Plugin;

impl Guest for Plugin {
    fn describe() -> Descriptor {
        Descriptor {
            description: "Component analyzer".into(),
            category: "security".into(),
            subcategory: "scripts".into(),
            maturity: "stable".into(),
            provider: "example".into(),
            recommended: false,
            explanation: "Checks script invocations.".into(),
            help: "Review the indicated command.".into(),
            documentation_url: String::new(),
            fix_availability: "none".into(),
            options_schema: "{}".into(),
            examples: vec![],
            name: "misbehave".to_string(),
            requires: vec![Fact::Scripts],
        }
    }

    fn analyze(input: Input) -> Outcome {
        let partial = matches!(input.options.as_str(), "partial" | "\"partial\"");
        match analyze(input) {
            Ok(diagnostics) => Outcome {
                diagnostics,
                error: partial.then(|| "analysis incomplete on purpose".to_string()),
            },
            Err(error) => Outcome {
                diagnostics: Vec::new(),
                error: Some(error),
            },
        }
    }
}

fn analyze(input: Input) -> Result<Vec<Diagnostic>, String> {
    match input.options.as_str() {
        #[cfg(target_arch = "wasm32")]
        "trap" => core::arch::wasm32::unreachable(),
        #[cfg(not(target_arch = "wasm32"))]
        "trap" => std::process::abort(),
        "" | "panic" => panic!("plugin panic requested"),
        "hang" => loop {
            core::hint::black_box(());
        },
        "allocate" => {
            let mut blocks: Vec<Vec<u8>> = Vec::new();
            loop {
                blocks.push(vec![1u8; 1 << 20]);
                core::hint::black_box(&blocks);
            }
        }
        "exit" => std::process::exit(3),
        "file" => match std::fs::read_to_string("/etc/hostname") {
            Ok(content) => Err(format!("read host file: {content}")),
            Err(error) => Err(format!("filesystem denied: {error}")),
        },
        "environment" => Err(format!(
            "environment: {:?}",
            std::env::vars().collect::<Vec<_>>()
        )),
        "log" => {
            log(&format!("analyzing {}", input.path));
            println!("stdout line");
            eprintln!("stderr line");
            Ok(Vec::new())
        }
        "direct-log-output" => {
            let chunk = "x".repeat(65536);
            for _ in 0..32 {
                log(&chunk);
            }
            Ok(Vec::new())
        }
        "error" => Err("analysis failed on purpose".to_string()),
        "output" => {
            use std::io::Write;
            let chunk = vec![b'x'; 65536];
            for _ in 0..32 {
                // The host must retain the limit failure even if the guest ignores it.
                let _ = std::io::stdout().write_all(&chunk);
            }
            Ok(Vec::new())
        }
        "partial-log" => {
            use std::io::Write;
            print!("partial stdout");
            let _ = std::io::stdout().flush();
            Ok(Vec::new())
        }
        _ => Ok(vec![Diagnostic {
            reference: None,
            explanation: String::new(),
            help: String::new(),
            documentation_url: String::new(),
            related: Vec::new(),
            rule: String::new(),
            code: "MB001".to_string(),
            severity: "warning".to_string(),
            message: format!("options {:?}", input.options),
            path: String::new(),
            start: Position { line: 1, column: 1 },
            end: Position { line: 1, column: 3 },
            fixes: Vec::new(),
        }]),
    }
}

#[cfg(target_arch = "wasm32")]
export!(Plugin);
