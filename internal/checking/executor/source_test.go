package executor

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

func TestSourceInputKindAndParseStatus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, content, kind, status string
	}{
		{"workflow", "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n", "workflow", "complete"},
		{"partial workflow", "on: push\njobs: {test: {steps: []}}\n", "workflow", "partial"},
		{"broken workflow", "jobs: [", "workflow", "failed"},
		{"action", "name: test\ndescription: test\nruns: {using: composite, steps: []}\n", "action", "complete"},
		{"partial action", "name: test\ninputs: [wrong]\nruns: {using: composite, steps: []}\n", "action", "partial"},
		{"broken action", "runs: [", "action", "failed"},
		{"empty action", "", "action", "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content := []byte(tc.content)
			document := parsing.Workflow(content)
			if tc.kind == "action" {
				document = parsing.Action("action.yml", content)
			}
			input := SourceInput(document, "source.yml", content, source.Reference{})
			if input.Kind != tc.kind || input.ParseStatus != tc.status || input.Content != tc.content {
				t.Fatalf("kind=%s status=%s content=%q", input.Kind, input.ParseStatus, input.Content)
			}
		})
	}
}
