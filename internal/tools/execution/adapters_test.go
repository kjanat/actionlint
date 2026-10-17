package execution

import (
	"context"
	"errors"
	"testing"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/documentparse"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/tools"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

type partialFactAnalyzer struct {
	t       *testing.T
	failure error
}

func (a partialFactAnalyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "test"}
}
func (a partialFactAnalyzer) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	a.t.Helper()
	if len(input.Scripts) != 1 || input.Scripts[0].Text != "echo ok" {
		a.t.Fatalf("changed script facts: %+v", input)
	}
	return []diagnostic.Diagnostic{{Rule: "test", Message: "partial finding", Start: diagnostic.Position{Line: 1, Column: 1}}}, a.failure
}

type partialTool struct {
	t       *testing.T
	failure error
}

func (a partialTool) Analyze(_ context.Context, batch tools.Batch) ([]tools.Finding, error) {
	a.t.Helper()
	if len(batch.Scripts) != 1 || batch.Scripts[0].Text != "echo ok" {
		a.t.Fatalf("changed tool batch: %+v", batch)
	}
	return []tools.Finding{{Rule: "test", Message: "partial finding", Start: diagnostic.Position{Line: 1, Column: 1}, Script: 0, Content: batch.Scripts[0].Content}}, a.failure
}

func TestScriptAdaptersPreserveOwnershipAndPartialFindings(t *testing.T) {
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n")
	parsed := documentparse.Workflow("ci.yml", content)
	run := parsed.Workflow.Jobs["test"].Steps[0].Exec
	owner := &findingmodel.Owner{Job: "test", Step: parsed.Workflow.Jobs["test"].Steps[0].Pos}
	failure := errors.New("partial analysis failure")
	for name, adapter := range map[string]Analyzer{
		"fact": FactAnalyzer{Analyzer: partialFactAnalyzer{t: t, failure: failure}},
		"tool": Tool{Runner: partialTool{t: t, failure: failure}},
	} {
		t.Run(name, func(t *testing.T) {
			findings, err := (Service{Analyzers: []Analyzer{adapter}}).Run(t.Context(), Batch{Scripts: []Unit{{Run: run.(*workflowlang.ExecRun), Path: "ci.yml", Content: content, Owner: owner}}})
			if !errors.Is(err, failure) || len(findings) != 1 || findings[0].Message != "partial finding" || findings[0].Owner != owner || findings[0].IgnoreOrigin != nil || string(findings[0].Source) != string(content) {
				t.Fatalf("partial finding lost its independent owner or source: %+v %v", findings, err)
			}
		})
	}
}
