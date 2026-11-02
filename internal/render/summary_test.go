package render

import (
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
)

func TestCheckSummaryGroupsCodesWithinRules(t *testing.T) {
	summary := NewCheckSummary(4, []diagnostic.Diagnostic{
		{Path: "b.yml", Rule: "action", Code: "missing-file"},
		{Path: "a.yml", Rule: "action", Code: "runtime-deprecated"},
		{Path: "b.yml", Rule: "action", Code: "missing-file"},
		{Path: "b.yml", Rule: "other-plugin", Code: "missing-file"},
		{Path: "a.yml", Rule: "syntax-check"},
		{Path: "a.yml", Rule: "syntax-check", Code: "syntax-check"},
	})
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
	var out strings.Builder
	if err := summary.WriteDetails(&out); err != nil {
		t.Fatal(err)
	}
	if want := "\nFiles:\n  3  a.yml\n  3  b.yml\n\nDiagnostics:\n  2  action/missing-file\n  2  syntax-check\n  1  action/runtime-deprecated\n  1  other-plugin/missing-file\n"; out.String() != want {
		t.Fatalf("details: %q, want %q", out.String(), want)
	}
	if err := summary.WriteDetails(failingRenderWriter{}); err == nil {
		t.Fatal("lost summary write error")
	}
	if got := NewCheckSummary(4, nil); got.Files != 4 || got.Findings != 0 || len(got.ByFile) != 0 || len(got.ByDiagnostic) != 0 {
		t.Fatalf("clean result has finding groups: %+v", got)
	}
}
