use actionlint_extension_api::{Dependency, Guest, Input, InputKind, Position, SourceReference};
use pinned_actions::Plugin;

#[test]
fn reports_only_unpinned_repository_dependencies() {
    for (kind, uses, reports) in [
        ("action", "actions/checkout@v4", true),
        (
            "workflow",
            "owner/repo/.github/workflows/build.yml@master",
            true,
        ),
        ("action", "owner/repo@abcdef0", true),
        (
            "action",
            "owner/repo@0123456789abcdef0123456789abcdef01234567",
            false,
        ),
        (
            "workflow",
            "owner/repo/.github/workflows/build.yml@ABCDEF0123456789ABCDEF0123456789ABCDEF01",
            false,
        ),
        (
            "action",
            "owner/repo@gggggggggggggggggggggggggggggggggggggggg",
            true,
        ),
        ("action", "./actions/setup", false),
        ("action", "docker://alpine@sha256:abc", false),
        ("action", "owner/repo", false),
        ("unknown", "owner/repo@v1", false),
    ] {
        let reference = SourceReference {
            document: "workflow".into(),
            revision: "captured-revision".into(),
            node: "uses-node".into(),
            address: "jobs.build.steps[0].uses".into(),
        };
        let start = Position {
            line: 8,
            column: 15,
        };
        let end = Position {
            line: 8,
            column: 35,
        };
        let result = Plugin::analyze(Input {
            reference: reference.clone(),
            path: "workflow.yml".into(),
            // The check must use supplied facts rather than parse source text.
            content: "not YAML: [".into(),
            kind: "workflow".into(),
            parse_status: "parsed".into(),
            available_inputs: vec![InputKind::Dependencies],
            options: String::new(),
            scripts: Vec::new(),
            invoked: Vec::new(),
            mappings: Vec::new(),
            dependencies: vec![Dependency {
                reference: reference.clone(),
                uses: uses.into(),
                kind: kind.into(),
                start,
                end,
            }],
            selected_scripts: None,
            action_inputs: Vec::new(),
        });
        assert!(result.error.is_none(), "{uses}: {:?}", result.error);
        assert_eq!(
            result.diagnostics.len(),
            usize::from(reports),
            "{kind}: {uses}"
        );
        if reports {
            let finding = &result.diagnostics[0];
            let source = finding
                .reference
                .as_ref()
                .expect("captured source reference");
            assert_eq!(source.document, reference.document);
            assert_eq!(source.revision, reference.revision);
            assert_eq!(source.node, reference.node);
            assert_eq!(source.address, reference.address);
            assert_eq!(
                (finding.start.line, finding.start.column),
                (start.line, start.column)
            );
            assert_eq!(
                (finding.end.line, finding.end.column),
                (end.line, end.column)
            );
            assert_eq!(finding.path, "workflow.yml");
            assert_eq!(finding.code, "unpinned-dependency");
            assert_eq!(finding.severity, "warning");
            assert!(finding.fixes.is_empty());
        }
    }
}
