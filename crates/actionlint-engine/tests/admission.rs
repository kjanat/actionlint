use actionlint_engine::{Analyzer, WorkflowAnalyzer, analyze_workflow};
use actionlint_model::{
    analysis::{
        Analysis, Applicability, Completion, Diagnostic, Fix, Related, Severity, Unavailable,
    },
    source::{Edit, Snapshot},
};

#[derive(Clone, Copy)]
enum Violation {
    Primary,
    Related,
    Fix,
    Overlap,
    EmptyFix,
}

impl WorkflowAnalyzer for Violation {
    fn analyze(&self, input: &actionlint_model::analysis::Input<'_>) -> Result<Analysis, String> {
        let source = input.source;
        let valid = Diagnostic {
            rule: "test/valid".into(),
            message: "valid".into(),
            severity: Severity::Error,
            primary: source.span(0..1).unwrap(),
            related: Vec::new(),
            fixes: Vec::new(),
        };
        let stale = Snapshot::new(source.uri().into(), "stale".into())
            .span(0..1)
            .unwrap();
        let mut invalid = valid.clone();
        invalid.rule = "test/invalid".into();
        match self {
            Self::Primary => invalid.primary = stale,
            Self::Related => invalid.related.push(Related {
                message: "old revision".into(),
                location: stale,
            }),
            Self::Fix => invalid.fixes.push(Fix {
                title: "stale fix".into(),
                applicability: Applicability::Automatic,
                edits: vec![Edit {
                    span: stale,
                    replacement: "x".into(),
                }],
            }),
            Self::Overlap => invalid.fixes.push(Fix {
                title: "overlapping fix".into(),
                applicability: Applicability::Automatic,
                edits: vec![
                    Edit {
                        span: source.span(0..2).unwrap(),
                        replacement: "x".into(),
                    },
                    Edit {
                        span: source.span(1..3).unwrap(),
                        replacement: "y".into(),
                    },
                ],
            }),
            Self::EmptyFix => invalid.fixes.push(Fix {
                title: "no edits".into(),
                applicability: Applicability::Automatic,
                edits: Vec::new(),
            }),
        }
        Ok(Analysis {
            diagnostics: vec![valid, invalid],
            completion: Completion::Complete,
        })
    }
}

#[test]
fn invalid_native_output_is_rejected_without_losing_valid_findings() {
    for violation in [
        Violation::Primary,
        Violation::Related,
        Violation::Fix,
        Violation::Overlap,
        Violation::EmptyFix,
    ] {
        let source = Snapshot::new("workflow.yml".into(), "jobs: {build: {needs: []}}".into());
        let run = analyze_workflow(
            source,
            &[Analyzer {
                name: "native",
                implementation: &violation,
            }],
        );
        assert_eq!(run.analysis.diagnostics.len(), 1);
        assert_eq!(run.analysis.diagnostics[0].rule, "test/valid");
        assert_eq!(run.failures.len(), 1);
        assert_eq!(
            run.analysis.completion,
            Completion::Incomplete(vec![Unavailable::Invalid])
        );
    }
}
