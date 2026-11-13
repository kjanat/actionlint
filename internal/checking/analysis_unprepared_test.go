package checking

import (
	"context"
	"errors"
	"testing"

	composites "actionlint.kjanat.dev/v2/internal/checking/executor/composites"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
	scriptexecution "actionlint.kjanat.dev/v2/internal/checking/scripts"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/results"
)

type sourceOnlyProbe struct{ content string }

func (sourceOnlyProbe) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "source-probe", Category: "correctness", RequiredInputs: []rulemeta.InputKind{rulemeta.Source}}
}

func (p sourceOnlyProbe) CheckDocument(_ context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	if input.Content != p.content || len(input.AvailableInputs) != 1 {
		return nil, errors.New("source-only input changed")
	}
	return []results.Diagnostic{{Message: "reported source finding"}}, nil
}

func TestSourceAnalyzerSurvivesUnavailableComposition(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"workflow":         "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n",
		"partial workflow": "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok, timeout-minutes: invalid}]}}\n",
		"action":           "name: example\ndescription: example\nruns: {using: node24, main: index.js}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			parsed := parsing.Workflow([]byte(content))
			if name == "action" {
				parsed = parsing.Action("action.yml", []byte(content))
			}
			if parsed.Facts == nil {
				t.Fatal("fixture has no semantic facts")
			}
			if name == "partial workflow" && len(parsed.SyntaxErrors) == 0 {
				t.Fatal("fixture did not reproduce a recovered parse")
			}
			input, err := composites.CheckInput(composites.Result{}, "ci.yml", "ci.yml", []byte(content), parsed, nil)
			if !errors.Is(err, scriptexecution.ErrInvalidBatch) {
				t.Fatalf("missing preparation error: %v", err)
			}
			var used []rulemeta.Descriptor
			findings, err := runDocumentAnalyzers(t.Context(), []checkinputs.Registration{{Checker: sourceOnlyProbe{content}}}, input, nil, &used)
			if err != nil || len(findings) != 1 || findings[0].Message != "reported source finding" {
				t.Fatalf("source analyzer skipped: %v %v", findings, err)
			}
		})
	}
}
