package security

import (
	"strings"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

func TestResolvedReferenceUsesCapturedTargets(t *testing.T) {
	commit := strings.Repeat("a", 40)
	ref := sourcepos.Reference{Document: "workflow.yml", Revision: "captured", Node: "step"}
	dependency := workflowfacts.Dependency{Reference: ref, Uses: "owner/repo/subpath@v1", Start: sourcepos.Position{Line: 3, Column: 15}, End: sourcepos.Position{Line: 3, Column: 40}}
	input := checkinputs.Input{Path: "workflow.yml", Reference: ref, Dependencies: []workflowfacts.Dependency{dependency}}
	rule := NewResolvedReferenceCheck()
	if findings, err := rule.CheckDocument(t.Context(), input); err != nil || len(findings) != 0 {
		t.Fatalf("unresolved reference became a pin: %v %v", findings, err)
	}
	input.ResolvedDependencies = []workflowfacts.ResolvedDependency{{Dependency: dependency, Ref: "v1", Commit: commit, RepositoryID: 42, ObservedAt: "2026-10-16T00:00:00Z"}}
	findings, err := rule.CheckDocument(t.Context(), input)
	if err != nil || len(findings) != 1 || findings[0].Reference.Revision != ref.Revision || findings[0].Fixes[0].Edits[0].Replacement != `"owner/repo/subpath@`+commit+`"` {
		t.Fatalf("captured pin lost its source or target: %v %v", findings, err)
	}
	input.ResolvedDependencies[0].Commit = ""
	input.ResolvedDependencies[0].Tree = commit
	if findings, err := rule.CheckDocument(t.Context(), input); err != nil || len(findings) != 0 {
		t.Fatalf("tree became a commit pin: %v %v", findings, err)
	}
}
