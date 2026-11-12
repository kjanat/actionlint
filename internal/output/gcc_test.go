package output

import (
	"strings"
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestGCCOutput(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, severity, code, message, want string
	}{
		{"error", "error", "unknown-variable", "Unknown variable", "error: Unknown variable [unknown-variable]"},
		{"default error", "", "", "Unknown variable", "error: Unknown variable [expression-reference]"},
		{"warning", "warning", "SC2086", "Double quote this", "warning: Double quote this [SC2086]"},
		{"information", "info", "SC2035", "Use ./*glob*", "note: Use ./*glob* [SC2035]"},
		{"note", "note", "", "Useful information", "note: Useful information [expression-reference]"},
		{"multiline", "error", "", "first\r\nsecond\nthird\rfourth; keep  spaces", "error: first second third fourth; keep  spaces [expression-reference]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			renderer, err := New(FormatGCC, "", false, testVersion, nil)
			if err != nil {
				t.Fatal(err)
			}
			finding := results.Diagnostic{
				Path: `C:\repo with space\wørkflow.yml`, Rule: "expression-reference", Code: tc.code,
				Severity: tc.severity, Message: tc.message, Start: sourcepos.Position{Line: 7, Column: 12}, Snippet: "not emitted",
			}
			var out strings.Builder
			if err := renderer.Render(&out, Result{Diagnostics: []results.Diagnostic{finding}}); err != nil {
				t.Fatal(err)
			}
			if got, want := out.String(), `C:\repo with space\wørkflow.yml:7:12: `+tc.want+"\n"; got != want {
				t.Fatalf("gcc=%q, want=%q", got, want)
			}
		})
	}
	var out strings.Builder
	if err := WriteGCC(&out, nil); err != nil || out.Len() != 0 {
		t.Fatalf("clean report: %q, %v", out.String(), err)
	}
}
