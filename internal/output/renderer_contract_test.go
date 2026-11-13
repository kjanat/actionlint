package output

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	commandstatus "actionlint.kjanat.dev/v2/internal/cli/exitcode"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestRendererRetainsRulesAcrossCalls(t *testing.T) {
	t.Parallel()
	renderer, err := New("", "{{range allKinds}}{{.Name}}:{{.Description}}\n{{end}}", false, func() string { return "test" }, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		var out strings.Builder
		result := report.Analysis{Rules: []rule.Descriptor{{Name: fmt.Sprintf("custom-%d", i), Description: "custom description"}}}
		if err := renderer.Render(&out, FromAnalysis(result)); err != nil {
			t.Fatal(err)
		}
		for n := 1; n <= i; n++ {
			if !strings.Contains(out.String(), fmt.Sprintf("custom-%d:custom description", n)) {
				t.Fatalf("formatter forgot a completed call: %s", &out)
			}
		}
	}
}

type failingRenderWriter struct{}

func (failingRenderWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
func TestRendererWriteErrors(t *testing.T) {
	t.Parallel()
	analysis := report.Analysis{FileCount: 1, Diagnostics: []results.Diagnostic{{Path: "input.yml", Rule: "expression-reference", Message: "unknown variable", Start: sourcepos.Position{Line: 1, Column: 1}, End: sourcepos.Position{Line: 1, Column: 2}}}}
	for _, format := range []Format{FormatText, FormatOneline, FormatGCC, FormatJSON, FormatJSONL, FormatSARIF, FormatGitHub} {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			renderer, err := New(format, "", false, func() string { return "test" }, nil)
			if err != nil {
				t.Fatal(err)
			}
			presentation := FromAnalysis(analysis)
			presentation.CheckResult = commandstatus.AnalysisResult(analysis)
			err = renderer.Render(failingRenderWriter{}, presentation)
			text := format == FormatText || format == FormatOneline
			if (err == nil) != text {
				t.Fatalf("write error contract changed: %v", err)
			}
		})
	}
}
