package output

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestSummaryDetails(t *testing.T) {
	t.Parallel()
	summary := report.CheckSummary{
		Files: 4, Findings: 6,
		ByFile: []report.FileFindingCount{{Path: "a.yml", Count: 3}, {Path: "b.yml", Count: 3}},
		ByDiagnostic: []report.DiagnosticCount{
			{Rule: "action", Code: "missing-file", Count: 2},
			{Rule: "syntax-check", Count: 2},
			{Rule: "action", Code: "runtime-deprecated", Count: 1},
			{Rule: "other-plugin", Code: "missing-file", Count: 1},
		},
	}
	var out strings.Builder
	if err := WriteSummaryDetails(&out, summary); err != nil {
		t.Fatal(err)
	}
	if want := "\nFiles:\n  3  a.yml\n  3  b.yml\n\nDiagnostics:\n  2  action/missing-file\n  2  syntax-check\n  1  action/runtime-deprecated\n  1  other-plugin/missing-file\n"; out.String() != want {
		t.Fatalf("details: %q, want %q", out.String(), want)
	}
	if err := WriteSummaryDetails(failingRenderWriter{}, summary); err == nil {
		t.Fatal("lost summary write error")
	}
}

func TestSummaryDetailsAlignCountsAndSeparateDependencies(t *testing.T) {
	t.Parallel()
	summary := report.CheckSummary{
		ByFile:           []report.FileFindingCount{{Path: "a.yml", Count: 34}, {Path: "b.yml", Count: 1}},
		DependencyByFile: []report.FileFindingCount{{Path: "remote.yml", Count: 12}, {Path: "other.yml", Count: 2}},
		ByDiagnostic:     []report.DiagnosticCount{{Rule: "shellcheck", Code: "SC2086", Origin: results.DependencyOrigin, Count: 34}, {Rule: "action", Count: 1}},
	}
	var out strings.Builder
	if err := WriteSummaryDetails(&out, summary); err != nil {
		t.Fatal(err)
	}
	want := "\nFiles:\n  34  a.yml\n   1  b.yml\n\nDependencies:\n  12  remote.yml\n   2  other.yml\n\nDiagnostics:\n  34  dependency:shellcheck/SC2086\n   1  action\n"
	if out.String() != want {
		t.Fatalf("summary details = %q; want %q", out.String(), want)
	}
}
