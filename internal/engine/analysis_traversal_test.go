package engine

import (
	"context"
	"errors"
	"io"
	"testing"

	"actionlint.kjanat.dev/internal/analyzerhost"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/fsread"
)

type phaseScriptAnalyzer struct {
	calls   int
	scripts int
	failure error
}

func (a *phaseScriptAnalyzer) AnalyzeScripts(_ context.Context, batch scriptBatch) ([]*findingmodel.Finding, error) {
	a.calls++
	a.scripts = len(batch.scripts)
	return nil, a.failure
}

func TestTraversalDefersScriptExecutionToScheduler(t *testing.T) {
	source := []byte(commandGoodWorkflow)
	parsed := parseDocument("workflow.yml", "workflow.yml", source)
	if parsed.workflow == nil {
		t.Fatalf("parse failed: %+v", parsed.findings)
	}
	scriptFailure := errors.New("script failure")
	script := &phaseScriptAnalyzer{failure: scriptFailure}
	proc := analyzerhost.NewProcessPool(t.Context(), 1)
	defer proc.Wait()
	defer func() {
		if err := proc.Finish(); err != nil {
			t.Error(err)
		}
	}()
	host := traversalHost{ctx: t.Context(), workingDir: t.TempDir(), reader: fsread.New(t.Context(), nil, nil), logOut: io.Discard, buildRules: func(ruleContext, bool) (builtinAnalyzers, error) {
		return builtinAnalyzers{scripts: []scriptAnalyzer{script}}, nil
	}}
	traversed, err := host.traverse("workflow.yml", host.workingDir, parsed.workflow, nil, nil, nil, nil, proc, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if script.calls != 0 {
		t.Fatal("traversal executed a script analyzer")
	}
	batch := scriptBatch{scripts: traversed.composites.scripts, scopes: traversed.composites.scriptScopes}
	if len(batch.scripts) != 1 {
		t.Fatalf("prepared scripts=%d", len(batch.scripts))
	}
	_, err = runScriptAnalyzers(t.Context(), traversed.builtins.scripts, batch)
	if !errors.Is(err, scriptFailure) || script.calls != 1 || script.scripts != 1 {
		t.Fatalf("execution: error=%v, analyzer=%+v", err, script)
	}
}
