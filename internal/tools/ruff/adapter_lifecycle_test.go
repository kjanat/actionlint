package ruff

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/tools"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func TestRuffAnalyzerCompletesBeforeReturning(t *testing.T) {
	var workers sync.WaitGroup
	failure := errors.New("checker failed")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal([]Diagnostic{{Code: "F821", Message: "undefined", Filename: filepath.Join(cwd, "actionlint.py"), Location: Position{Row: 1, Column: 1}, EndLocation: Position{Row: 1, Column: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	checker := New(func(_ []string, _ string, callback func([]byte, error) error) {
		workers.Go(func() {
			if err := callback(payload, nil); err != nil {
				t.Error(err)
			}
		})
	}, func() error { workers.Wait(); return failure }, ruffExpressionEnd)
	analyzer := &ruffAnalyzer{checker: checker}
	pos := &workflowlang.Pos{Line: 1, Col: 1}
	python := "python"
	scripts := []tools.Script{{Text: "x", Key: pos, Value: pos, Shell: &python, Path: "first.yml"}, {Text: "x", Key: pos, Value: pos, Shell: &python, Path: "second.yml"}}
	findings, err := analyzer.Analyze(t.Context(), tools.Batch{Scripts: scripts})
	if !errors.Is(err, failure) || len(findings) != 2 {
		t.Fatalf("findings=%v error=%v", findings, err)
	}
	if findings[0].Path != "first.yml" || findings[1].Path != "second.yml" {
		t.Fatal(findings)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	findings, err = analyzer.Analyze(ctx, tools.Batch{Scripts: scripts})
	if !errors.Is(err, context.Canceled) || len(findings) != 0 {
		t.Fatalf("cancelled: %v %v", findings, err)
	}
}

func TestRuffAdapterMapsBothEndpoints(t *testing.T) {
	source := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: |\n          print(missing)\n")
	parsed, failures := workflowlang.Parse(source)
	if len(failures) != 0 {
		t.Fatal(failures)
	}
	step := parsed.Jobs["test"].Steps[0]
	run := step.Exec.(*workflowlang.ExecRun)
	python := "python"
	for _, endpoints := range []struct {
		name       string
		start, end int
	}{
		{"mapped", 1, 1}, {"unmapped start", 999, 1}, {"unmapped end", 1, 999},
	} {
		t.Run(endpoints.name, func(t *testing.T) {
			checker := NewBackend(t.Context(), testBackend(func(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
				return []analyzer.ScriptDiagnostic{{Filename: stdinFilename, Code: "F821", Message: "undefined name", Severity: "warning", URL: "https://docs.astral.sh/ruff/rules/undefined-name/", Location: Position{Row: endpoints.start, Column: 7}, EndLocation: Position{Row: endpoints.end, Column: 14}}}, nil
			}), ruffExpressionEnd)
			adapter := &ruffAnalyzer{checker: checker}
			findings, err := adapter.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{{Text: run.Run.Value, Source: run.Source, Key: run.RunPos, Value: run.Run.Pos, Shell: &python, Path: "workflow.yml", Content: source}}})
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) != 1 {
				t.Fatalf("want one finding: %+v", findings)
			}
			finding := findings[0]
			if finding.DocumentationURL != "https://docs.astral.sh/ruff/rules/undefined-name/" {
				t.Fatalf("documentation URL lost: %+v", finding)
			}
			wantStart, wantEnd := (diagnostic.Position{Line: 8, Column: 17}), (diagnostic.Position{Line: 8, Column: 24})
			if endpoints.name != "mapped" {
				wantStart = diagnostic.Position{Line: 7, Column: 9}
				wantEnd = diagnostic.Position{}
			}
			if finding.Start != wantStart || finding.End != wantEnd || finding.Code != "F821" || finding.Severity != "warning" || finding.Path != "workflow.yml" || finding.Script != 0 || string(finding.Content) != string(source) {
				t.Fatalf("source mapping or diagnostic metadata changed: %+v", finding)
			}
		})
	}
}
