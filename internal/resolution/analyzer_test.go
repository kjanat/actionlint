package resolution

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/resolver"
	"actionlint.kjanat.dev/source"
)

type fakeResolver struct {
	requests []resolver.Request
	err      error
	tree     bool
}

func (f *fakeResolver) Resolve(_ context.Context, r resolver.Request) (resolver.Result, error) {
	f.requests = append(f.requests, r)
	result := resolver.Result{Version: 1, Repository: r.Repository, Evidence: json.RawMessage(`{}`), RepositoryID: 42, ObservedAt: "2026-10-16T00:00:00Z"}
	if !f.tree {
		result.Commit = strings.Repeat("a", 40)
	} else {
		result.Tree = strings.Repeat("b", 40)
	}
	return result, f.err
}
func TestConsumerAndPinProvenance(t *testing.T) {
	provider := &fakeResolver{}
	rule := Analyzer{provider}
	ref := source.Reference{Document: "workflow.yml", Revision: "revision", Node: "step", Address: "jobs.build.steps[0]"}
	input := analyzer.Input{Path: "workflow.yml", Reference: ref, Dependencies: []analyzer.Dependency{
		{Reference: ref, Uses: "owner/repo/path@v1", Kind: "action", Start: diagnostic.Position{Line: 1, Column: 1}, End: diagnostic.Position{Line: 1, Column: 24}},
		{Reference: ref, Uses: "owner/repo/.github/workflows/build.yml@v1", Kind: "workflow"},
		{Uses: "./local", Kind: "action"},
	}}
	findings, err := rule.Analyze(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 2 || provider.requests[0].Kind != "action" || provider.requests[1].Kind != "workflow" {
		t.Fatal(provider.requests)
	}
	if len(findings) != 2 || findings[0].Reference.Node != "step" || findings[0].Fixes[0].Revisions[input.Path] != "revision" || !strings.Contains(findings[0].Fixes[0].Edits[0].Replacement, "owner/repo/path@"+strings.Repeat("a", 40)) {
		t.Fatal(findings)
	}
	provider.tree = true
	findings, err = rule.Analyze(t.Context(), input)
	if err != nil || len(findings) != 0 {
		t.Fatalf("tree became commit fix: %v %v", findings, err)
	}
}
