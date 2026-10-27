use actionlint_app::{
    Application,
    editor::{Change, Session},
};
use actionlint_model::source::Encoding;

#[test]
fn unsaved_text_runs_through_component_and_stale_results_are_not_published() {
    let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"));
    let app =
        Application::load(&[root.join("../../target/components/workflow-rules.wasm")]).unwrap();
    let uri = "file:///not-on-disk/workflow.yml";
    let mut session = Session::default();
    session
        .open(
            uri.into(),
            1,
            "jobs:\n  build:\n    needs: missing\n".into(),
        )
        .unwrap();
    let captured = session.capture(uri).unwrap();
    let completed = captured.analyze(&app);
    let published = session.publish(completed).unwrap();
    assert!(published.run.failures.is_empty());
    assert_eq!(published.run.analysis.diagnostics.len(), 1);
    assert_eq!(
        published.run.analysis.diagnostics[0].primary.source().uri(),
        uri
    );
    assert_eq!(
        published.run.analysis.diagnostics[0].primary.text(),
        "missing"
    );

    let captured = session.capture(uri).unwrap();
    session
        .change(
            uri,
            2,
            vec![Change::Replace("jobs:\n  build:\n    needs: []\n".into())],
            Encoding::Utf16,
        )
        .unwrap();
    assert!(session.publish(captured.analyze(&app)).is_none());
    let current = session.capture(uri).unwrap().analyze(&app);
    let published = session.publish(current).unwrap();
    assert_eq!(published.version, 2);
    assert!(published.run.analysis.diagnostics.is_empty());
}
