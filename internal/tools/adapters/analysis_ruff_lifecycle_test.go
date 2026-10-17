package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"actionlint.kjanat.dev/internal/ruff"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func TestRuffAnalyzerCompletesBeforeReturning(t *testing.T) {
	var workers sync.WaitGroup
	failure := errors.New("checker failed")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal([]ruff.Diagnostic{{Code: "F821", Message: "undefined", Filename: filepath.Join(cwd, "actionlint.py"), Location: ruff.Position{Row: 1, Column: 1}, EndLocation: ruff.Position{Row: 1, Column: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	checker := ruff.New(func(_ []string, _ string, callback func([]byte, error) error) {
		workers.Go(func() {
			if err := callback(payload, nil); err != nil {
				t.Error(err)
			}
		})
	}, func() error { workers.Wait(); return failure }, ruffExpressionEnd)
	analyzer := &ruffAnalyzer{checker: checker}
	pos := &workflowlang.Pos{Line: 1, Col: 1}
	run := &workflowlang.ExecRun{Run: &workflowlang.String{Value: "x", Pos: pos}, RunPos: pos}
	python := "python"
	scripts := []scriptUnit{{run: run, shell: &python, path: "first.yml"}, {run: run, shell: &python, path: "second.yml"}}
	findings, err := analyzer.AnalyzeScripts(t.Context(), scriptBatch{scripts: scripts})
	if !errors.Is(err, failure) || len(findings) != 2 {
		t.Fatalf("findings=%v error=%v", findings, err)
	}
	if findings[0].Path != "first.yml" || findings[1].Path != "second.yml" {
		t.Fatal(findings)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	findings, err = analyzer.AnalyzeScripts(ctx, scriptBatch{scripts: scripts})
	if !errors.Is(err, context.Canceled) || len(findings) != 0 {
		t.Fatalf("cancelled: %v %v", findings, err)
	}
}
