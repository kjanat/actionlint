package application

import (
	"context"
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/composition"
	"actionlint.kjanat.dev/v2/rule"
)

type sourceOnlyProbe struct{ content string }

func (sourceOnlyProbe) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "source-probe", Requires: []analyzer.Fact{analyzer.Source}}
}

func (p sourceOnlyProbe) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	if input.Content != p.content || len(input.Available) != 1 {
		return nil, errors.New("source-only input changed")
	}
	return []diagnostic.Diagnostic{{Message: "retained source finding"}}, nil
}

func TestSourceAnalyzerSurvivesUnavailableComposition(t *testing.T) {
	for name, content := range map[string]string{
		"workflow":         "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n",
		"partial workflow": "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok, timeout-minutes: invalid}]}}\n",
		"action":           "name: example\ndescription: example\nruns: {using: node24, main: index.js}\n",
	} {
		t.Run(name, func(t *testing.T) {
			parsed := documentparse.Workflow("ci.yml", []byte(content))
			if name == "action" {
				parsed = documentparse.Action("action.yml", "action.yml", []byte(content))
			}
			if parsed.Facts() == nil {
				t.Fatal("fixture has no semantic facts")
			}
			if name == "partial workflow" && len(parsed.Findings) == 0 {
				t.Fatal("fixture did not reproduce a recovered parse")
			}
			input, err := (composition.Result{}).AnalyzerInput("ci.yml", "ci.yml", []byte(content), parsed, nil)
			if !errors.Is(err, scriptexecution.ErrInvalidBatch) {
				t.Fatalf("missing preparation error: %v", err)
			}
			var used []rule.Descriptor
			findings, err := runDocumentAnalyzers(t.Context(), []analyzer.Registration{{Analyzer: sourceOnlyProbe{content}}}, input, nil, &used)
			if err != nil || len(findings) != 1 || findings[0].Message != "retained source finding" {
				t.Fatalf("source analyzer skipped: %v %v", findings, err)
			}
		})
	}
}
