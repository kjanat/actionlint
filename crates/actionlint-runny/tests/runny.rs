use actionlint_runny::{ParseOutcome, Runny};

#[test]
fn original_runner_parses_a_batch_without_evaluating_unknown_context() {
    let artifact = std::env::var_os("RUNNY_WASM")
        .map(std::path::PathBuf::from)
        .unwrap_or_else(|| {
            std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
                .join("../../target/providers/runny-v0.2.0/runny-v0.2.0-expression-full.wasm")
        });
    let bytes = std::fs::read(artifact).unwrap();
    let runny = Runny::compile(&bytes).unwrap();
    let result = runny
        .parse(&["github.ref", "1 +", "contains('🦀', '🦀')"])
        .unwrap();
    assert_eq!(
        result.runner_revision,
        "67f01c276e0a91d967ba499ce4cdc9a95efa0262"
    );
    assert_eq!(result.outcomes[0], ParseOutcome::Valid);
    assert!(
        matches!(&result.outcomes[1], ParseOutcome::Rejected { message, .. } if !message.is_empty())
    );
    assert_eq!(result.outcomes[2], ParseOutcome::Valid);
    let again = runny.parse(&["null"]).unwrap();
    assert_eq!(again.outcomes, vec![ParseOutcome::Valid]);
    assert!(runny.parse(&[]).is_err());

    let source = actionlint_model::source::Snapshot::new(
        "workflow.yml".into(),
        "jobs:\n  build:\n    name: \"\\u0024{{ 1 + }}\"\n".into(),
    );
    let run = actionlint_engine::analyze_workflow(
        source.clone(),
        &[actionlint_engine::Analyzer {
            name: "runny",
            implementation: &runny,
        }],
    );
    assert!(run.failures.is_empty(), "{:?}", run.failures);
    assert_eq!(run.analysis.diagnostics.len(), 1);
    let diagnostic = &run.analysis.diagnostics[0];
    assert_eq!(diagnostic.rule, "expression/syntax");
    assert_eq!(diagnostic.primary.text(), "\\u0024{{ 1 + }}");
    assert_eq!(diagnostic.primary.source(), &source);
}
