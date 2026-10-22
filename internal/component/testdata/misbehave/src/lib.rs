wit_bindgen::generate!({
    path: "../../../../plugins/wit",
    world: "plugin",
});

use exports::kjanat::actionlint::analyzer::{Descriptor, Diagnostic, Fact, Guest, Input, Position};
use kjanat::actionlint::host::log;

struct Plugin;

impl Guest for Plugin {
    fn describe() -> Descriptor {
        Descriptor {
            name: "misbehave".to_string(),
            requires: vec![Fact::Scripts],
        }
    }

    fn analyze(input: Input) -> Result<Vec<Diagnostic>, String> {
        match input.options.as_str() {
            "trap" => core::arch::wasm32::unreachable(),
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
            "rule-identities" => Ok(["", "undeclared-check", "misbehave", "shellcheck", ""]
                .into_iter()
                .map(|rule| Diagnostic {
                    rule: rule.to_string(),
                    code: "MB001".to_string(),
                    severity: "warning".to_string(),
                    message: format!("rule {rule:?}"),
                    path: input.path.clone(),
                    start: Position { line: 1, column: 1 },
                    end: Position { line: 1, column: 3 },
                    fixes: Vec::new(),
                })
                .collect()),
            "error" => Err("analysis failed on purpose".to_string()),
            _ => Ok(vec![Diagnostic {
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
}

export!(Plugin);
