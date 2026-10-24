//! Finite compatibility probe, not an actionlint port or a production host.
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use wasmtime::component::{Component, Linker, ResourceTable};
use wasmtime::{Config, Engine, Store, StoreLimits, StoreLimitsBuilder};
use wasmtime_wasi::{WasiCtx, WasiCtxView, WasiView};

wasmtime::component::bindgen!({ path: "../../crates/actionlint-extension-api/wit", world: "runner-plugin" });
use exports::kjanat::actionlint::runner_provider::{
    Answer, CaseSensitivity, Constraint, Freshness, Os, Position, Request, Resolution, Selector,
    Snapshot, Term,
};

struct Host {
    wasi: WasiCtx,
    table: ResourceTable,
    limits: StoreLimits,
}
impl WasiView for Host {
    fn ctx(&mut self) -> WasiCtxView<'_> {
        WasiCtxView {
            ctx: &mut self.wasi,
            table: &mut self.table,
        }
    }
}

fn normalize(answer: Answer) -> Value {
    let (kind, candidates, rejections, reason) = match answer.resolution {
        Resolution::Resolved(candidates) => ("resolved", candidates.into_iter().map(|c| json!({
            "Name": c.name, "OS": match c.os { Os::Unknown => "unknown", Os::Linux => "linux", Os::Macos => "macos", Os::Windows => "windows" },
            "Architecture": c.architecture, "DefaultShell": c.default_shell,
            "CaseSensitivity": match c.case_sensitivity { CaseSensitivity::Unknown => "unknown", CaseSensitivity::Sensitive => "sensitive", CaseSensitivity::Insensitive => "insensitive" }, "ShIsDash": c.sh_is_dash, "Ephemeral": c.ephemeral,
        })).collect(), Vec::new(), String::new()),
        Resolution::NoMatch(rejections) => ("no-match", Vec::new(), rejections.into_iter().map(|r| json!({
            "Reason": r.reason, "Position": { "line": r.position.line, "column": r.position.column }
        })).collect(), String::new()),
        Resolution::Unknown(reason) => ("unknown", Vec::new(), Vec::new(), reason),
        Resolution::NotApplicable => ("not-applicable", Vec::new(), Vec::new(), String::new()),
    };
    let e = answer.evidence;
    json!({ "Kind": kind, "Candidates": candidates, "Rejections": rejections, "Reason": reason,
        "Evidence": { "Dataset": e.dataset, "Scope": e.scope, "ObtainedAt": e.obtained_at,
            "Freshness": match e.freshness { Freshness::Fresh => "fresh", Freshness::Stale => "stale", Freshness::Missing => "missing" }, "Complete": e.complete } })
}

fn main() -> wasmtime::Result<()> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let [artifact, snapshot] = args.as_slice() else {
        wasmtime::bail!("usage: actionlint-runner-host COMPONENT SNAPSHOT");
    };
    let bytes = std::fs::read(artifact)?;
    let mut config = Config::new();
    config.wasm_component_model(true).consume_fuel(true);
    let engine = Engine::new(&config)?;
    let component = Component::new(&engine, &bytes)?;
    let mut linker = Linker::new(&engine);
    wasmtime_wasi::p2::add_to_linker_sync(&mut linker)?;
    let wasi = WasiCtx::builder()
        .allow_tcp(false)
        .allow_udp(false)
        .allow_ip_name_lookup(false)
        .build();
    let mut store = Store::new(
        &engine,
        Host {
            wasi,
            table: ResourceTable::new(),
            limits: StoreLimitsBuilder::new().memory_size(64 << 20).build(),
        },
    );
    store.limiter(|host| &mut host.limits);
    store.set_fuel(10_000_000)?;
    let plugin = RunnerPlugin::instantiate(&mut store, &component, &linker)?;
    let provider = plugin.kjanat_actionlint_runner_provider();
    let descriptor = provider.call_describe(&mut store)?;
    let selectors = [
        Term::Literal("buildkite-windows".into()),
        Term::Literal("missing-queue".into()),
        Term::Expression("${{ vars.RUNNER }}".into()),
    ]
    .into_iter()
    .map(|value| Selector {
        incomplete: false,
        labels: vec![Constraint {
            value,
            position: Position {
                line: 4,
                column: 14,
            },
        }],
        group: None,
        expression: String::new(),
    })
    .collect();
    let request = Request {
        target: "buildkite".into(),
        scope: "fixture".into(),
        snapshot: Snapshot {
            content: std::fs::read_to_string(snapshot)?,
            scope: "fixture".into(),
            obtained_at: "2026-10-19T00:00:00Z".into(),
            freshness: Freshness::Fresh,
            complete: true,
        },
        selectors,
    };
    let outcome = provider.call_resolve(&mut store, &request)?;
    if let Some(error) = outcome.error {
        wasmtime::bail!(error);
    }
    println!(
        "{}",
        json!({ "Digest": format!("{:x}", Sha256::digest(&bytes)), "Name": descriptor.name, "Version": descriptor.version, "Targets": descriptor.targets, "Answers": outcome.answers.into_iter().map(normalize).collect::<Vec<_>>() })
    );
    Ok(())
}
