package shellcheck

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestShellcheckRetainsPreparationFailureOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	failure := errors.New("invalid ShellCheck configuration")
	prepared := 0
	checker := &shellcheckAnalyzer{forConfig: func(*Config) (shellcheckTransport, error) {
		prepared++
		cancel()
		return shellcheckTransport{}, failure
	}}
	_, err := checker.AnalyzeScripts(ctx, scriptBatch{scopes: []*scriptScope{{}, {}}})
	if prepared != 1 || !errors.Is(err, failure) || !errors.Is(err, context.Canceled) {
		t.Fatalf("preparation failure or cancellation lost: prepared=%d err=%v", prepared, err)
	}
}

func TestShellcheckFallsBackWhenEitherEndpointIsUnmapped(t *testing.T) {
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n")
	document := documentparse.Workflow("ci.yml", content)
	if len(document.Findings) != 0 {
		t.Fatal(document.Findings)
	}
	run := document.Workflow.Jobs["test"].Steps[0].Exec.(*workflowlang.ExecRun)
	var registry documentparse.SourceSnapshots
	snapshot, err := registry.Document("ci.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	step, err := snapshot.StepView(document.Workflow.Jobs["test"].Steps[0])
	if err != nil {
		t.Fatal(err)
	}
	ownedRun, _ := step.Run()

	for _, endpoints := range []struct {
		name       string
		start, end int
	}{
		{"mapped", 1, 1}, {"unmapped start", 999, 1}, {"unmapped end", 1, 999},
	} {
		t.Run(endpoints.name, func(t *testing.T) {
			cmd := analyzerhost.NewProcessPool(t.Context(), 1).EmbeddedCommand("shellcheck", func([]string, string) ([]byte, error) {
				return fmt.Appendf(nil, `{"comments":[{"line":1,"column":%d,"endLine":%d,"endColumn":2,"code":9999,"level":"warning","message":"range"}]}`, endpoints.start, endpoints.end), nil
			})
			invocation := &shellcheckRun{cmd: cmd, rcArgs: []string{"--norc"}}
			if err := invocation.runShellcheck(run.Run.Value, ownedRun.Source(), shellcheckShell{name: "bash {0}"}, runDirectory{directoryKnown, t.TempDir()}, run.RunPos, run.Run.Pos); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
			if len(invocation.errs) != 1 {
				t.Fatal(invocation.errs)
			}
			finding := invocation.errs[0]
			if endpoints.name == "mapped" {
				if finding.endPosition == nil {
					t.Fatalf("mapped range lost: %+v", finding)
				}
			} else if finding.Start.Line != run.RunPos.Line || finding.Start.Column != run.RunPos.Col || finding.endPosition != nil {
				t.Fatalf("unreliable range did not fall back to run key: %+v", finding)
			}
		})
	}
}

func TestShellcheckRetainsFindingsWhenAnotherScopeHasInvalidConfig(t *testing.T) {
	root := t.TempDir()
	invalid, err := config.Parse([]byte("tools: {shellcheck: {config: 'missing.shellcheckrc'}}"))
	if err != nil {
		t.Fatal(err)
	}
	validScope := &scriptScope{paths: runPaths{analysis: root, workspace: root}}
	invalidScope := &scriptScope{paths: validScope.paths, config: invalid, composite: true}
	pos := &workflowlang.Pos{Line: 1, Col: 1}
	makeScript := func(scope *scriptScope) scriptUnit {
		return scriptUnit{scope: scope, text: "echo test", key: pos, value: pos, execution: scriptExecution{shell: shellcheckShell{name: "bash {0}"}, directory: runDirectory{directoryKnown, root}}}
	}
	checker := &shellcheckAnalyzer{cmd: analyzerhost.NewProcessPool(t.Context(), 1).EmbeddedCommand("shellcheck", func([]string, string) ([]byte, error) {
		return []byte(`{"comments":[{"line":1,"endLine":1,"column":1,"endColumn":2,"code":9999,"level":"warning","message":"reported"}]}`), nil
	})}
	findings, err := checker.AnalyzeScripts(t.Context(), scriptBatch{scopes: []*scriptScope{validScope, invalidScope}, scripts: []scriptUnit{makeScript(validScope), makeScript(invalidScope)}})
	if err == nil || len(findings) != 1 {
		t.Fatalf("partial result lost: findings=%+v err=%v", findings, err)
	}
}
