use actionlint_engine::{WorkflowAnalyzer, analyze_workflow};
use actionlint_model::{
    analysis::{Analysis, Completion, Input, Unavailable},
    source::Snapshot,
};

struct EmptyAnalyzer;

#[test]
fn source_only_analysis_does_not_require_yaml_or_workflow_facts() {
    let source = Snapshot::new("notes.txt".into(), "not yaml: [".into());
    let run = analyze_workflow(
        source,
        &[actionlint_engine::Analyzer {
            name: "source-only",
            implementation: &EmptyAnalyzer,
        }],
    );
    assert!(run.failures.is_empty());
    assert!(run.analysis.diagnostics.is_empty());
    assert_eq!(run.analysis.completion, Completion::Complete);
}

impl WorkflowAnalyzer for EmptyAnalyzer {
    fn analyze(&self, _: &Input<'_>) -> Result<Analysis, String> {
        Ok(Analysis {
            diagnostics: Vec::new(),
            completion: Completion::Complete,
        })
    }
}

#[test]
fn analyzer_cannot_erase_missing_input() {
    let source = Snapshot::new(
        "workflow.yml".into(),
        "jobs:\n  build:\n    needs: ${{ inputs.jobs }}\n".into(),
    );
    let run = analyze_workflow(
        source,
        &[actionlint_engine::Analyzer {
            name: "test",
            implementation: &EmptyAnalyzer,
        }],
    );
    assert!(run.failures.is_empty());
    assert_eq!(
        run.analysis.completion,
        Completion::Incomplete(vec![Unavailable::Unresolved])
    );
}

#[test]
fn syntax_failure_is_not_a_clean_analysis() {
    let source = Snapshot::new("workflow.yml".into(), "jobs: [".into());
    let run = analyze_workflow(
        source,
        &[actionlint_engine::Analyzer {
            name: "test",
            implementation: &EmptyAnalyzer,
        }],
    );
    assert!(!run.analysis.diagnostics.is_empty());
    assert_eq!(
        run.analysis.completion,
        Completion::Incomplete(vec![Unavailable::Invalid])
    );
}

struct FailingAnalyzer;
impl WorkflowAnalyzer for FailingAnalyzer {
    fn analyze(&self, _: &Input<'_>) -> Result<Analysis, String> {
        Err("intentional failure".into())
    }
}

#[test]
fn analyzer_failures_are_named_and_do_not_count_as_complete() {
    let source = Snapshot::new("workflow.yml".into(), "jobs: {build: {needs: []}}".into());
    let run = analyze_workflow(
        source,
        &[
            actionlint_engine::Analyzer {
                name: "broken",
                implementation: &FailingAnalyzer,
            },
            actionlint_engine::Analyzer {
                name: "working",
                implementation: &EmptyAnalyzer,
            },
        ],
    );
    assert_eq!(run.failures.len(), 1);
    assert!(
        matches!(&run.failures[0], actionlint_engine::Failure::Analyzer { name, message } if name == "broken" && message == "intentional failure")
    );
    assert_eq!(
        run.analysis.completion,
        Completion::Incomplete(vec![Unavailable::Unresolved])
    );
}

#[test]
fn no_analyzers_is_not_full_coverage() {
    let source = Snapshot::new("workflow.yml".into(), "jobs: {build: {needs: []}}".into());
    let run = analyze_workflow(source, &[]);
    assert_eq!(
        run.analysis.completion,
        Completion::Incomplete(vec![Unavailable::Unsupported])
    );
}
