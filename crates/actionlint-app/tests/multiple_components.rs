use actionlint_app::Application;
use actionlint_model::{analysis::Completion, source::Snapshot};

#[test]
fn broken_component_does_not_discard_other_components_findings() {
    let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../../target/components");
    let app = Application::load(&[
        root.join("workflow-rules.wasm"),
        root.join("incompatible-analysis.wasm"),
        root.join("c-rule.wasm"),
    ])
    .unwrap();
    let run = app.analyze_snapshot(Snapshot::new(
        "file:///workflow.yml".into(),
        "jobs:\n  build:\n    needs: build\n".into(),
    ));
    assert_eq!(
        run.analysis
            .diagnostics
            .iter()
            .map(|diagnostic| diagnostic.rule.as_str())
            .collect::<Vec<_>>(),
        vec!["workflow/dependency-cycle", "workflow/self-dependency"]
    );
    assert_eq!(run.failures.len(), 1);
    assert!(
        matches!(&run.failures[0], actionlint_engine::Failure::Analyzer { name, .. } if name.ends_with("incompatible-analysis.wasm"))
    );
    assert!(matches!(run.analysis.completion, Completion::Incomplete(_)));
}

#[test]
fn empty_selection_is_rejected_before_analysis() {
    assert!(Application::load(&[]).is_err());
}
