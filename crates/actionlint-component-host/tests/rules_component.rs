use actionlint_component_host::{ComponentHost, bindings::actionlint::extension::types::*};

fn component() -> Vec<u8> {
    std::fs::read(concat!(
        env!("CARGO_MANIFEST_DIR"),
        "/../../target/components/workflow-rules.wasm"
    ))
    .expect("build target/components/workflow-rules.wasm before running component conformance")
}

fn request(snapshot: u64, dependency: &str) -> Request {
    let text = format!("jobs:\n  test:\n    needs: {dependency}\n");
    let start = u32::try_from(text.find(dependency).unwrap()).unwrap();
    Request {
        expressions: Vec::new(),
        snapshot,
        sources: vec![Source { id: 7, text }],
        jobs: Jobs::Known(vec![Job {
            name: Name {
                value: "test".into(),
                location: Range {
                    source: 7,
                    start: 8,
                    end: 12,
                },
            },
            needs: Dependencies::Known(vec![Name {
                value: dependency.into(),
                location: Range {
                    source: 7,
                    start,
                    end: start + u32::try_from(dependency.len()).unwrap(),
                },
            }]),
        }]),
    }
}

#[test]
fn actual_component_reports_a_typed_source_bound_diagnostic() {
    let host = ComponentHost::new().unwrap().compile(&component()).unwrap();
    let input = request(41, "missing");
    let result = host.analyze(&input).unwrap().unwrap();
    assert_eq!(result.snapshot, 41);
    assert!(matches!(result.completion, Completion::Complete));
    assert_eq!(result.diagnostics.len(), 1);
    let diagnostic = &result.diagnostics[0];
    assert_eq!(diagnostic.rule, "workflow/undefined-dependency");
    let start = usize::try_from(diagnostic.primary.start).unwrap();
    let end = usize::try_from(diagnostic.primary.end).unwrap();
    assert_eq!(&input.sources[0].text[start..end], "missing");
}

#[test]
fn consecutive_calls_have_separate_results_and_snapshot_identity() {
    let host = ComponentHost::new().unwrap().compile(&component()).unwrap();
    assert_eq!(
        host.analyze(&request(1, "missing"))
            .unwrap()
            .unwrap()
            .diagnostics
            .len(),
        1
    );
    let mut clean = request(2, "test");
    if let Jobs::Known(jobs) = &mut clean.jobs {
        jobs[0].needs = Dependencies::Known(Vec::new());
    }
    let next = host.analyze(&clean).unwrap().unwrap();
    assert_eq!(next.snapshot, 2);
    assert!(next.diagnostics.is_empty());
}

#[test]
fn unknown_inputs_are_incomplete_and_bad_source_ids_are_rejected() {
    let host = ComponentHost::new().unwrap().compile(&component()).unwrap();
    let mut input = request(3, "missing");
    input.jobs = Jobs::Unavailable(Unavailable::Unresolved);
    let result = host.analyze(&input).unwrap().unwrap();
    assert!(matches!(result.completion, Completion::Incomplete(_)));
    let mut input = request(4, "missing");
    input.sources.clear();
    assert!(matches!(
        host.analyze(&input).unwrap(),
        Err(Failure::InvalidInput(_))
    ));
}

#[test]
fn workflow_text_reaches_component_with_original_source_identity() {
    use actionlint_engine::analyze_workflow;
    use actionlint_model::source::Snapshot;
    let component = ComponentHost::new().unwrap().compile(&component()).unwrap();
    let source = Snapshot::new(
        "file:///workflow.yml".into(),
        "# café\r\njobs:\r\n  build:\r\n    needs: [missing]\r\n".into(),
    );
    let run = analyze_workflow(
        source.clone(),
        &[actionlint_engine::Analyzer {
            name: "test",
            implementation: &component,
        }],
    );
    assert!(run.failures.is_empty(), "{:?}", run.failures);
    assert_eq!(run.analysis.diagnostics.len(), 1);
    let diagnostic = &run.analysis.diagnostics[0];
    assert_eq!(diagnostic.rule, "workflow/undefined-dependency");
    assert_eq!(diagnostic.primary.source(), &source);
    assert_eq!(&source.text()[diagnostic.primary.bytes()], "missing");
    assert_eq!(
        run.analysis.completion,
        actionlint_model::analysis::Completion::Complete
    );

    let source = Snapshot::new(
        "file:///workflow.yml".into(),
        "jobs:\n  build:\n    needs: ${{ inputs.jobs }}\n".into(),
    );
    let run = analyze_workflow(
        source,
        &[actionlint_engine::Analyzer {
            name: "test",
            implementation: &component,
        }],
    );
    assert!(run.failures.is_empty(), "{:?}", run.failures);
    assert!(matches!(
        run.analysis.completion,
        actionlint_model::analysis::Completion::Incomplete(_)
    ));
}

#[test]
fn dependency_cycle_crosses_component_boundary_with_related_source_locations() {
    use actionlint_engine::analyze_workflow;
    use actionlint_model::source::Snapshot;
    let compiled = ComponentHost::new().unwrap().compile(&component()).unwrap();
    let source = Snapshot::new(
        "file:///workflow.yml".into(),
        "jobs:\n  build:\n    needs: test\n  test:\n    needs: build\n".into(),
    );
    let run = analyze_workflow(
        source.clone(),
        &[actionlint_engine::Analyzer {
            name: "test",
            implementation: &compiled,
        }],
    );
    assert!(run.failures.is_empty(), "{:?}", run.failures);
    assert_eq!(run.analysis.diagnostics.len(), 1);
    let diagnostic = &run.analysis.diagnostics[0];
    assert_eq!(diagnostic.rule, "workflow/dependency-cycle");
    assert_eq!(diagnostic.primary.text(), "build");
    assert_eq!(
        diagnostic.primary.bytes().start,
        source.text().rfind("build").unwrap()
    );
    assert_eq!(
        diagnostic
            .related
            .iter()
            .map(|related| related.location.text())
            .collect::<Vec<_>>(),
        vec!["build", "test"]
    );
    assert!(
        diagnostic
            .related
            .iter()
            .all(|related| related.location.source() == &source)
    );
}
