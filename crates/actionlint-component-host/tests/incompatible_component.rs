use actionlint_component_host::{ComponentHost, bindings::actionlint::extension::types::*};

#[test]
fn incompatible_analysis_signature_is_rejected_before_execution() {
    let bytes = std::fs::read(concat!(
        env!("CARGO_MANIFEST_DIR"),
        "/../../target/components/incompatible-analysis.wasm"
    ))
    .expect("build the incompatible component fixture before running contract tests");
    let request = Request {
        expressions: Vec::new(),
        snapshot: 1,
        sources: Vec::new(),
        jobs: Jobs::Known(Vec::new()),
    };
    let error = ComponentHost::new()
        .unwrap()
        .compile(&bytes)
        .unwrap()
        .analyze(&request)
        .unwrap_err();
    assert!(
        format!("{error:#}").contains("type mismatch with parameters"),
        "{error:#}"
    );
}
