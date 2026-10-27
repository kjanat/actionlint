use actionlint_component_host::{ComponentHost, bindings::actionlint::extension::types::*};

#[test]
fn independent_c_component_receives_decoded_expression_segments_and_original_ranges() {
    let bytes = std::fs::read(concat!(
        env!("CARGO_MANIFEST_DIR"),
        "/../../target/components/c-expression.wasm"
    ))
    .unwrap();
    let host = ComponentHost::new().unwrap().compile(&bytes).unwrap();
    let source = actionlint_model::source::Snapshot::new(
        "file:///workflow.yml".into(),
        "jobs:\n  build:\n    name: \"\\u0024{{ github.actor }}\"\n".into(),
    );
    let run = actionlint_engine::analyze_workflow(
        source.clone(),
        &[actionlint_engine::Analyzer {
            name: "c-expression",
            implementation: &host,
        }],
    );
    assert!(run.failures.is_empty());
    assert_eq!(run.analysis.diagnostics.len(), 1);
    let diagnostic = &run.analysis.diagnostics[0];
    assert_eq!(diagnostic.rule, "test/transformed-expression");
    assert_eq!(diagnostic.message, "$");
    assert_eq!(diagnostic.primary.text(), "\\u0024");
    assert_eq!(diagnostic.primary.source(), &source);
}

#[test]
fn independent_c_component_checks_input_and_returns_a_source_range() {
    let bytes = std::fs::read(concat!(
        env!("CARGO_MANIFEST_DIR"),
        "/../../target/components/c-rule.wasm"
    ))
    .expect("build the C conformance component before running this test");
    let host = ComponentHost::new().unwrap().compile(&bytes).unwrap();
    let text = "jobs:\n  build:\n    needs: BUILD\n";
    let name = |value: &str, start, end| Name {
        value: value.into(),
        location: Range {
            source: 8,
            start,
            end,
        },
    };
    let mut request = Request {
        expressions: Vec::new(),
        snapshot: 90,
        sources: vec![Source {
            id: 8,
            text: text.into(),
        }],
        jobs: Jobs::Known(vec![Job {
            name: name("build", 8, 13),
            needs: Dependencies::Known(vec![name("BUILD", 26, 31)]),
        }]),
    };
    let result = host.analyze(&request).unwrap().unwrap();
    assert_eq!(result.snapshot, 90);
    assert!(matches!(result.completion, Completion::Complete));
    assert_eq!(result.diagnostics.len(), 1);
    let diagnostic = &result.diagnostics[0];
    assert_eq!(diagnostic.rule, "workflow/self-dependency");
    assert_eq!(diagnostic.fixes.len(), 1);
    assert_eq!(diagnostic.fixes[0].edits[0].replacement, "[]");
    assert!(matches!(
        diagnostic.fixes[0].applicability,
        Applicability::RequiresReview
    ));
    assert_eq!(diagnostic.related.len(), 1);
    assert_eq!(diagnostic.related[0].location.start, 8);
    assert_eq!(diagnostic.related[0].location.end, 13);
    let start = usize::try_from(diagnostic.primary.start).unwrap();
    let end = usize::try_from(diagnostic.primary.end).unwrap();
    assert_eq!(&text[start..end], "BUILD");

    let mut sequence = request.clone();
    sequence.sources[0].text = "jobs:\n  build:\n    needs: [BUILD]\n".into();
    if let Jobs::Known(jobs) = &mut sequence.jobs
        && let Dependencies::Known(needs) = &mut jobs[0].needs
    {
        needs[0].location.start = 27;
        needs[0].location.end = 32;
    }
    let sequence_result = host.analyze(&sequence).unwrap().unwrap();
    assert_eq!(sequence_result.diagnostics.len(), 1);
    assert!(sequence_result.diagnostics[0].fixes.is_empty());

    request.sources[0].id = 9;
    let error = host.analyze(&request).unwrap_err();
    assert!(format!("{error:#}").contains("unknown source ID"));
    request.sources[0].id = 8;

    request.jobs = Jobs::Known(vec![Job {
        name: name("build", 8, 13),
        needs: Dependencies::Known(Vec::new()),
    }]);
    assert!(
        host.analyze(&request)
            .unwrap()
            .unwrap()
            .diagnostics
            .is_empty()
    );
    request.jobs = Jobs::Unavailable(Unavailable::Unresolved);
    assert!(matches!(
        host.analyze(&request).unwrap().unwrap().completion,
        Completion::Incomplete(_)
    ));
}
