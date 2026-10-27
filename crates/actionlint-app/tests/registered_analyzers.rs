use actionlint_app::{Application, Registration, editor::Session};
use actionlint_engine::{Failure, WorkflowAnalyzer};
use actionlint_model::{
    analysis::{Analysis, Completion, Diagnostic, Input, Severity, Unavailable},
    source::Snapshot,
};

struct Expressions;

impl WorkflowAnalyzer for Expressions {
    fn analyze(&self, input: &Input<'_>) -> Result<Analysis, String> {
        Ok(Analysis {
            diagnostics: input
                .expressions
                .iter()
                .map(|expression| Diagnostic {
                    rule: "test/expression".into(),
                    message: expression.text.text().into(),
                    severity: Severity::Information,
                    primary: expression.location.clone(),
                    related: Vec::new(),
                    fixes: Vec::new(),
                })
                .collect(),
            completion: Completion::Complete,
        })
    }
}

struct Failed;

impl WorkflowAnalyzer for Failed {
    fn analyze(&self, _: &Input<'_>) -> Result<Analysis, String> {
        Err("provider unavailable".into())
    }
}

#[test]
fn registered_analyzers_share_snapshot_and_editor_admission() {
    let application = Application::new(vec![
        Registration::new("expressions", Expressions),
        Registration::new("offline-provider", Failed),
    ])
    .unwrap();
    let uri = "file:///workflow.yml";
    let text = "jobs:\n  build:\n    name: \"\\u0024{{ github.actor }}\"\n";
    let source = Snapshot::new(uri.into(), text.into());
    let direct = application.analyze_snapshot(source.clone());
    assert_eq!(direct.analysis.diagnostics.len(), 1);
    assert_eq!(direct.analysis.diagnostics[0].primary.source(), &source);
    assert_eq!(
        direct.analysis.diagnostics[0].message,
        "${{ github.actor }}"
    );
    assert_eq!(
        direct.analysis.diagnostics[0].primary.text(),
        "\\u0024{{ github.actor }}"
    );
    assert_eq!(
        direct.analysis.completion,
        Completion::Incomplete(vec![Unavailable::Unresolved])
    );
    assert!(matches!(
        direct.failures.as_slice(),
        [Failure::Analyzer { name, message }]
            if name == "offline-provider" && message == "provider unavailable"
    ));
    let mut session = Session::default();
    session.open(uri.into(), 7, text.into()).unwrap();
    let completed = session.capture(uri).unwrap().analyze(&application);
    let publication = session.publish(completed).unwrap();
    assert_eq!(publication.version, 7);
    assert_eq!(publication.run.analysis, direct.analysis);
}

#[test]
fn analyzer_names_must_identify_one_provider() {
    assert!(Application::new(Vec::new()).is_err());
    assert!(Application::new(vec![Registration::new("  ", Expressions)]).is_err());
    assert!(
        Application::new(vec![
            Registration::new("same", Expressions),
            Registration::new("same", Failed),
        ])
        .is_err()
    );
}

#[test]
fn native_and_component_analyzers_share_the_application() {
    let component = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../../target/components/workflow-rules.wasm");
    let host = actionlint_component_host::ComponentHost::new().unwrap();
    let compiled = host.compile(&std::fs::read(component).unwrap()).unwrap();
    let application = Application::new(vec![
        Registration::new("expressions", Expressions),
        Registration::new("workflow-rules", compiled),
    ])
    .unwrap();
    let run = application.analyze_snapshot(Snapshot::new(
        "file:///workflow.yml".into(),
        "jobs:\n  build:\n    name: ${{ github.actor }}\n    needs: missing\n".into(),
    ));
    assert!(run.failures.is_empty());
    assert_eq!(run.analysis.completion, Completion::Complete);
    assert_eq!(run.analysis.diagnostics.len(), 2);
    assert_eq!(run.analysis.diagnostics[0].rule, "test/expression");
    assert_eq!(run.analysis.diagnostics[1].primary.text(), "missing");
}
