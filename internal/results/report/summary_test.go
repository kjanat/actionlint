package report

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/results"
)

func TestCheckSummaryGroupsCodesWithinRules(t *testing.T) {
	t.Parallel()
	summary := NewCheckSummary(Analysis{FileCount: 4, Diagnostics: []results.Diagnostic{
		{Path: "b.yml", Rule: "action", Code: "missing-file"},
		{Path: "a.yml", Rule: "action", Code: "runtime-deprecated"},
		{Path: "b.yml", Rule: "action", Code: "missing-file"},
		{Path: "b.yml", Rule: "other-plugin", Code: "missing-file"},
		{Path: "a.yml", Rule: "syntax-check"},
		{Path: "a.yml", Rule: "syntax-check", Code: "syntax-check"},
	}})
	want := CheckSummary{
		Files: 4, Findings: 6,
		ByFile: []FileFindingCount{{Path: "a.yml", Count: 3}, {Path: "b.yml", Count: 3}},
		ByDiagnostic: []DiagnosticCount{
			{Rule: "action", Code: "missing-file", Count: 2},
			{Rule: "syntax-check", Count: 2},
			{Rule: "action", Code: "runtime-deprecated", Count: 1},
			{Rule: "other-plugin", Code: "missing-file", Count: 1},
		},
	}
	if !reflect.DeepEqual(summary, want) {
		t.Fatalf("summary = %+v, want %+v", summary, want)
	}
	if got := NewCheckSummary(Analysis{FileCount: 4}); got.Files != 4 || got.Findings != 0 || len(got.ByFile) != 0 || len(got.ByDiagnostic) != 0 {
		t.Fatalf("clean result has finding groups: %+v", got)
	}
}
