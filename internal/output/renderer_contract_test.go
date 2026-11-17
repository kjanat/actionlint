package output

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/cli/exitcode"

	"actionlint.kjanat.dev/v2/internal/model/outputformat"
	"actionlint.kjanat.dev/v2/internal/model/rule"
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
	analysis := report.Analysis{FileCount: 1, Diagnostics: []results.PublishedDiagnostic{{Path: "input.yml", Rule: "expression-reference", Message: "unknown variable", Start: sourcetext.Position{Line: 1, Column: 1}, End: sourcetext.Position{Line: 1, Column: 2}}}}
	for _, format := range []outputformat.Format{outputformat.Text, outputformat.Oneline, outputformat.GCC, outputformat.JSON, outputformat.JSONL, outputformat.SARIF, outputformat.GitHub} {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			renderer, err := New(format, "", false, func() string { return "test" }, nil)
			if err != nil {
				t.Fatal(err)
			}
			presentation := FromAnalysis(analysis)
			presentation.CheckResult = exitcode.AnalysisResult(analysis)
			err = renderer.Render(failingRenderWriter{}, presentation)
			text := format == outputformat.Text || format == outputformat.Oneline
			if (err == nil) != text {
				t.Fatalf("write error contract changed: %v", err)
			}
		})
	}
}
