package checking

import (
	"context"
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/executor/composites"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/results"
)

type sourceOnlyProbe struct{ content string }

func (sourceOnlyProbe) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "source-probe", Category: "correctness", RequiredInputs: []rule.InputKind{rule.Source}}
}

func (p sourceOnlyProbe) AnalyzeDocument(_ context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
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
			input, err := composites.CheckInput(composites.Composition{}, "ci.yml", "ci.yml", []byte(content), parsed, nil)
			if !errors.Is(err, scriptanalysis.ErrInvalidBatch) {
				t.Fatalf("missing preparation error: %v", err)
			}
			var used []rule.Descriptor
			findings, err := runDocumentAnalyzers(t.Context(), []results.AnalyzerRegistration{{Analyzer: sourceOnlyProbe{content}}}, input, nil, &used)
			if err != nil || len(findings) != 1 || findings[0].Message != "reported source finding" {
				t.Fatalf("source analyzer skipped: %v %v", findings, err)
			}
		})
	}
}
