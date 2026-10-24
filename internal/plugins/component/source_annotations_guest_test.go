package component_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/plugins/component"
	"actionlint.kjanat.dev/v2/source"
)

// Exercise the actual optional import through canonical lowering and lifting.
// The guest also probes an unauthorized revision before its legitimate queries.
func TestGuestReadsOnlyCurrentScriptAnnotations(t *testing.T) {
	guest := loadPlugin(t, "source-annotations", component.Options{})
	root := source.Reference{Document: "ci.yml", Revision: "workflow", Node: "step", Address: "jobs.test.steps[0]"}
	action := source.Reference{Document: "action.yml", Revision: "action", Node: "step", Address: "runs.steps[0]"}
	input := analyzer.Input{
		Path:    "ci.yml",
		Scripts: []analyzer.Script{{Reference: root, Text: "echo root", Comments: []analyzer.SourceComment{{Text: "# root", Start: diagnostic.Position{Line: 2, Column: 7}}}}},
		Invoked: []analyzer.Script{{Reference: action, Text: "echo action", Path: "action.yml", Comments: []analyzer.SourceComment{{Text: "# action", Start: diagnostic.Position{Line: 6, Column: 19}, Placement: analyzer.TrailingComment}}}},
	}
	findings, err := guest.Analyze(t.Context(), input)
	if err != nil || len(findings) != 2 {
		t.Fatalf("findings=%+v, error=%v", findings, err)
	}
	for i, want := range []struct {
		text, code, path string
		start            diagnostic.Position
		reference        source.Reference
	}{
		{"# root", "leading", "", diagnostic.Position{Line: 2, Column: 7}, root},
		{"# action", "trailing", "action.yml", diagnostic.Position{Line: 6, Column: 19}, action},
	} {
		finding := findings[i]
		if finding.Message != want.text || finding.Code != want.code || finding.Path != want.path || finding.Start != want.start || finding.End != want.start || finding.Reference == nil || *finding.Reference != want.reference {
			t.Fatalf("finding=%+v, want %+v", finding, want)
		}
	}
	input.Scripts[0].Comments = nil
	input.Invoked[0].Comments = nil
	if findings, err := guest.Analyze(t.Context(), input); err != nil || len(findings) != 0 {
		t.Fatalf("annotations leaked across guest instances: %+v, %v", findings, err)
	}
}
