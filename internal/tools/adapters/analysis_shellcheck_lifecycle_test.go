package adapters

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/internal/analyzerhost"
	config "actionlint.kjanat.dev/internal/config"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func TestShellcheckFallsBackWhenEitherEndpointIsUnmapped(t *testing.T) {
	workflow, failures := workflowlang.Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n"))
	if len(failures) != 0 {
		t.Fatal(failures)
	}
	run := workflow.Jobs["test"].Steps[0].Exec.(*workflowlang.ExecRun)
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
			if err := invocation.runShellcheck(run.Run.Value, run.Source, shellcheckShell{name: "bash {0}"}, runDirectory{directoryKnown, t.TempDir()}, run.RunPos, run.Run.Pos); err != nil {
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
	run := &workflowlang.ExecRun{Run: &workflowlang.String{Value: "echo test", Pos: pos}, RunPos: pos}
	makeScript := func(scope *scriptScope) scriptUnit {
		return scriptUnit{scope: scope, run: run, execution: scriptExecution{shell: shellcheckShell{name: "bash {0}"}, directory: runDirectory{directoryKnown, root}}}
	}
	checker := &shellcheckAnalyzer{cmd: analyzerhost.NewProcessPool(t.Context(), 1).EmbeddedCommand("shellcheck", func([]string, string) ([]byte, error) {
		return []byte(`{"comments":[{"line":1,"endLine":1,"column":1,"endColumn":2,"code":9999,"level":"warning","message":"retained"}]}`), nil
	})}
	findings, err := checker.AnalyzeScripts(t.Context(), scriptBatch{scopes: []*scriptScope{validScope, invalidScope}, scripts: []scriptUnit{makeScript(validScope), makeScript(invalidScope)}})
	if err == nil || len(findings) != 1 {
		t.Fatalf("partial result lost: findings=%+v err=%v", findings, err)
	}
}
