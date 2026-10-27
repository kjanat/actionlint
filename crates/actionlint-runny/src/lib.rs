//! Runny's original runner parser through its WASIp1 batch protocol.

use actionlint_engine::WorkflowAnalyzer;
use actionlint_model::{
    analysis::{Analysis, Completion, Diagnostic, Input, Severity},
    expression::ExpressionKind,
};
use serde::{Deserialize, Serialize};
use wasmtime::{
    Config, Engine, InstancePre, Linker, Module, Store, StoreLimits, StoreLimitsBuilder,
};
use wasmtime_wasi::p1::{self, WasiP1Ctx};
use wasmtime_wasi::p2::pipe::{MemoryInputPipe, MemoryOutputPipe};
use wasmtime_wasi::{I32Exit, WasiCtxBuilder};

const INPUT_LIMIT: usize = 1024 * 1024;
const OUTPUT_LIMIT: usize = 16 * 1024 * 1024;

struct State {
    wasi: WasiP1Ctx,
    limits: StoreLimits,
}

pub struct Runny {
    engine: Engine,
    instance: InstancePre<State>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum ParseOutcome {
    Valid,
    Rejected {
        exception_type: String,
        message: String,
    },
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ParsedBatch {
    pub runner_revision: String,
    pub outcomes: Vec<ParseOutcome>,
}

impl WorkflowAnalyzer for Runny {
    fn analyze(&self, input: &Input<'_>) -> Result<Analysis, String> {
        let mut diagnostics = Vec::new();
        let mut pending = Vec::new();
        for expression in input.expressions {
            if expression.location.source() != input.source {
                return Err("expression belongs to another source revision".into());
            }
            let text = expression.text.text();
            let body = match expression.kind {
                ExpressionKind::Delimited => text
                    .strip_prefix("${{")
                    .and_then(|text| text.strip_suffix("}}"))
                    .ok_or("delimited expression has inconsistent text")?,
                ExpressionKind::BareCondition => text,
                ExpressionKind::Unterminated => {
                    diagnostics.push(Diagnostic {
                        rule: "expression/delimiter".into(),
                        message: "Expression is missing its closing delimiter.".into(),
                        severity: Severity::Error,
                        primary: expression.location.clone(),
                        related: Vec::new(),
                        fixes: Vec::new(),
                    });
                    continue;
                }
            };
            pending.push((expression, body));
        }
        if !pending.is_empty() {
            let bodies: Vec<_> = pending.iter().map(|(_, body)| *body).collect();
            let parsed = self.parse(&bodies)?;
            for ((expression, _), outcome) in pending.into_iter().zip(parsed.outcomes) {
                match outcome {
                    ParseOutcome::Valid => {}
                    ParseOutcome::Rejected {
                        exception_type,
                        message,
                    } => {
                        if exception_type != "GitHub.Actions.Expressions.ParseException" {
                            return Err(format!(
                                "Runny parser failed ({exception_type}): {message}"
                            ));
                        }
                        diagnostics.push(Diagnostic {
                            rule: "expression/syntax".into(),
                            message,
                            severity: Severity::Error,
                            primary: expression.location.clone(),
                            related: Vec::new(),
                            fixes: Vec::new(),
                        });
                    }
                }
            }
        }
        diagnostics.sort_by_key(|diagnostic| diagnostic.primary.bytes().start);
        Ok(Analysis {
            diagnostics,
            completion: Completion::Complete,
        })
    }
}

#[derive(Serialize)]
struct Request<'a> {
    id: String,
    operation: &'static str,
    engine: &'static str,
    input: &'a str,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct Response {
    id: String,
    protocol_version: u32,
    runner_sha: String,
    command: Option<String>,
    engines: Option<Vec<ParserResult>>,
    error: Option<RemoteError>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ParserResult {
    engine: String,
    parsed: bool,
    exception_type: Option<String>,
    exception: Option<String>,
}

#[derive(Deserialize)]
struct RemoteError {
    message: String,
}

impl Runny {
    pub fn compile(bytes: &[u8]) -> Result<Self, String> {
        let mut config = Config::new();
        config.consume_fuel(true);
        let engine = Engine::new(&config).map_err(display)?;
        let module = Module::new(&engine, bytes).map_err(display)?;
        let mut linker = Linker::new(&engine);
        p1::add_to_linker_sync(&mut linker, |state: &mut State| &mut state.wasi)
            .map_err(display)?;
        let instance = linker.instantiate_pre(&module).map_err(display)?;
        Ok(Self { engine, instance })
    }

    /// Validate syntax only: no missing execution context is evaluated as null.
    pub fn parse(&self, expressions: &[&str]) -> Result<ParsedBatch, String> {
        if expressions.is_empty() {
            return Err("Runny requires a nonempty expression batch".into());
        }
        let mut input = Vec::new();
        for (index, expression) in expressions.iter().enumerate() {
            if expression.len() > INPUT_LIMIT {
                return Err("Runny expression exceeds input limit".into());
            }
            serde_json::to_writer(
                &mut input,
                &Request {
                    id: index.to_string(),
                    operation: "parse-expr",
                    engine: "next",
                    input: expression,
                },
            )
            .map_err(display)?;
            input.push(b'\n');
            if input.len() > INPUT_LIMIT {
                return Err("Runny batch exceeds input limit".into());
            }
        }
        let output = MemoryOutputPipe::new(OUTPUT_LIMIT);
        let errors = MemoryOutputPipe::new(64 * 1024);
        let wasi = WasiCtxBuilder::new()
            .stdin(MemoryInputPipe::new(input))
            .stdout(output.clone())
            .stderr(errors.clone())
            .arg("runny")
            .build_p1();
        let state = State {
            wasi,
            limits: StoreLimitsBuilder::new()
                .memory_size(256 * 1024 * 1024)
                .table_elements(100_000)
                .instances(1)
                .memories(1)
                .build(),
        };
        let mut store = Store::new(&self.engine, state);
        store.limiter(|state| &mut state.limits);
        store.set_fuel(2_000_000_000).map_err(display)?;
        let instance = self.instance.instantiate(&mut store).map_err(display)?;
        let start = instance
            .get_typed_func::<(), ()>(&mut store, "_start")
            .map_err(display)?;
        if let Err(error) = start.call(&mut store, ())
            && !matches!(error.downcast_ref::<I32Exit>(), Some(I32Exit(0)))
        {
            return Err(format!(
                "Runny execution failed: {error:#}; {}",
                String::from_utf8_lossy(&errors.contents())
            ));
        }
        decode(&output.contents(), expressions.len())
    }
}

fn decode(output: &[u8], count: usize) -> Result<ParsedBatch, String> {
    let text = std::str::from_utf8(output).map_err(display)?;
    let mut revision = None;
    let mut outcomes = Vec::new();
    for (index, line) in text.lines().enumerate() {
        if index >= count {
            return Err("Runny returned extra responses".into());
        }
        let response: Response = serde_json::from_str(line).map_err(display)?;
        if response.protocol_version != 1 || response.id != index.to_string() {
            return Err("Runny response protocol or request identity mismatch".into());
        }
        if response.runner_sha.len() != 40
            || !response
                .runner_sha
                .bytes()
                .all(|byte| byte.is_ascii_hexdigit())
        {
            return Err("Runny returned an invalid runner revision".into());
        }
        if revision
            .as_ref()
            .is_some_and(|sha| sha != &response.runner_sha)
        {
            return Err("Runny changed runner revision within a batch".into());
        }
        revision = Some(response.runner_sha);
        if let Some(error) = response.error {
            return Err(format!("Runny rejected request {index}: {}", error.message));
        }
        if response.command.as_deref() != Some("parse-expr") {
            return Err("Runny returned a different operation".into());
        }
        let engines = response.engines.ok_or("Runny omitted parser results")?;
        let [result] = engines.as_slice() else {
            return Err("Runny must return exactly one selected engine".into());
        };
        if result.engine != "next" {
            return Err("Runny returned a different engine".into());
        }
        let outcome = match (result.parsed, &result.exception_type, &result.exception) {
            (true, None, None) => ParseOutcome::Valid,
            (false, Some(exception_type), Some(message)) => ParseOutcome::Rejected {
                exception_type: exception_type.clone(),
                message: message.clone(),
            },
            _ => return Err("Runny returned inconsistent parser status".into()),
        };
        outcomes.push(outcome);
    }
    if outcomes.len() != count {
        return Err("Runny omitted responses".into());
    }
    Ok(ParsedBatch {
        runner_revision: revision.ok_or("Runny omitted runner revision")?,
        outcomes,
    })
}

fn display(error: impl std::fmt::Display) -> String {
    error.to_string()
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn response() -> serde_json::Value {
        json!({
            "id": "0", "protocolVersion": 1,
            "runnerSha": "67f01c276e0a91d967ba499ce4cdc9a95efa0262",
            "command": "parse-expr", "engines": [{"engine": "next", "parsed": true}]
        })
    }

    #[test]
    fn transport_corruption_cannot_become_a_valid_parse() {
        let valid = response();
        assert_eq!(
            decode(valid.to_string().as_bytes(), 1).unwrap().outcomes,
            vec![ParseOutcome::Valid]
        );
        for (field, value) in [
            ("id", json!("other")),
            ("protocolVersion", json!(2)),
            ("runnerSha", json!("unknown")),
            ("command", json!("eval-expr")),
            ("engines", json!([])),
            ("error", json!({"message":"unsupported"})),
        ] {
            let mut malformed = response();
            malformed[field] = value;
            assert!(
                decode(malformed.to_string().as_bytes(), 1).is_err(),
                "{field}"
            );
        }
        assert!(decode(b"", 1).is_err());
        assert!(decode(format!("{valid}\n{valid}\n").as_bytes(), 1).is_err());
        assert!(decode(valid.to_string().as_bytes(), 2).is_err());
    }

    #[test]
    fn contradictions_and_mixed_revisions_are_rejected() {
        for engine in [
            json!({"engine":"legacy","parsed":true}),
            json!({"engine":"next","parsed":false}),
            json!({"engine":"next","parsed":true,"exception":"failure"}),
        ] {
            let mut malformed = response();
            malformed["engines"] = json!([engine]);
            assert!(decode(malformed.to_string().as_bytes(), 1).is_err());
        }
        let mut second = response();
        second["id"] = json!("1");
        second["runnerSha"] = json!("0000000000000000000000000000000000000000");
        assert!(decode(format!("{}\n{second}\n", response()).as_bytes(), 2).is_err());
    }
}
