package report

import (
	"strings"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestMergePreservesCompletionAndDiagnosticSources(t *testing.T) {
	t.Parallel()
	parts := []Analysis{
		{FileCount: 1, IncompleteChecks: []IncompleteCheck{{Rule: "first"}}, Diagnostics: []results.Diagnostic{{Message: "first"}}, Sources: []DiagnosticSource{{Content: []byte("first source")}}, Inputs: []string{"first"}, Rules: []rule.Descriptor{{Name: "rule"}}},
		{FileCount: 2, IncompleteChecks: []IncompleteCheck{{Rule: "second"}}, Diagnostics: []results.Diagnostic{{Message: "second"}}, Sources: []DiagnosticSource{{Content: []byte("second source")}}, Inputs: []string{"second"}, Rules: []rule.Descriptor{{Name: "rule"}}},
	}
	got, err := Merge(parts...)
	if err != nil {
		t.Fatal(err)
	}
	if got.FileCount != 3 || len(got.IncompleteChecks) != 2 || got.IncompleteChecks[0].Rule != "first" || got.IncompleteChecks[1].Rule != "second" || len(got.Diagnostics) != 2 || got.Diagnostics[0].Message != "first" || string(got.Sources[0].Content) != "first source" || got.Diagnostics[1].Message != "second" || string(got.Sources[1].Content) != "second source" || len(got.Rules) != 1 {
		t.Fatalf("merge lost or misaligned analysis data: %+v", got)
	}
}

func TestMergeRejectsConflictsAndUnalignedSources(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		parts []Analysis
		want  string
	}{
		{"capture", []Analysis{{CapturedSources: map[string]CapturedSource{"same": {Content: []byte("first")}}}, {CapturedSources: map[string]CapturedSource{"same": {Content: []byte("second")}}}}, "source \"same\" changed"},
		{"rule", []Analysis{{Rules: []rule.Descriptor{{Name: "rule", Description: "first"}}}, {Rules: []rule.Descriptor{{Name: "rule", Description: "second"}}}}, "conflicting metadata"},
		{"source alignment", []Analysis{{Diagnostics: []results.Diagnostic{{Message: "missing source"}}}}, "1 diagnostics and 0 source records"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Merge(tc.parts...); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got error %v, want %q", err, tc.want)
			}
		})
	}
}
