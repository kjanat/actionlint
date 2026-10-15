package engine

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
)

func TestShellcheckPipelineCompletesAfterTraversalFailure(t *testing.T) {
	failure := errors.New("stop traversal")
	var completed atomic.Bool
	result, err := Analyze(t.Context(), AnalysisRequest{
		Sources: []SourceUnit{{Path: "workflow.yml", Content: []byte(commandGoodWorkflow)}},
		ShellcheckRunner: func([]string, string) ([]byte, error) {
			completed.Store(true)
			return []byte(`{"comments":[{"line":3,"endLine":3,"column":1,"endColumn":2,"code":9999,"level":"warning","message":"retained finding"}]}`), nil
		},
		OnRulesCreated: func([]Rule) []Rule {
			return []Rule{&failAfterScheduling{RuleBase: NewRuleBase("fail", ""), failure: failure}}
		},
	})
	if !errors.Is(err, failure) || !completed.Load() || result == nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "shellcheck" {
		t.Fatalf("completion or partial findings lost: result=%+v err=%v completed=%v", result, err, completed.Load())
	}
}

func TestShellcheckFallsBackWhenEitherEndpointIsUnmapped(t *testing.T) {
	workflow, failures := Parse([]byte(commandGoodWorkflow))
	if len(failures) != 0 {
		t.Fatal(failures)
	}
	run := workflow.Jobs["test"].Steps[0].Exec.(*ExecRun)
	for _, endpoints := range []struct {
		name       string
		start, end int
	}{
		{"mapped", 1, 1}, {"unmapped start", 999, 1}, {"unmapped end", 1, 999},
	} {
		t.Run(endpoints.name, func(t *testing.T) {
			cmd := &externalCommand{runner: func([]string, string) ([]byte, error) {
				return []byte(fmt.Sprintf(`{"comments":[{"line":1,"column":%d,"endLine":%d,"endColumn":2,"code":9999,"level":"warning","message":"range"}]}`, endpoints.start, endpoints.end)), nil
			}}
			invocation := &shellcheckRun{cmd: cmd, rcArgs: []string{"--norc"}}
			if err := invocation.runShellcheck(run.Run.Value, run.source, shellcheckShell{name: "bash {0}"}, runDirectory{directoryKnown, t.TempDir()}, run.RunPos, run.Run.Pos); err != nil {
				t.Fatal(err)
			}
			if err := cmd.wait(); err != nil {
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
			} else if finding.Line != run.RunPos.Line || finding.Column != run.RunPos.Col || finding.endPosition != nil {
				t.Fatalf("unreliable range did not fall back to run key: %+v", finding)
			}
		})
	}
}

func TestShellcheckRetainsFindingsWhenAnotherScopeHasInvalidConfig(t *testing.T) {
	root := t.TempDir()
	invalid, err := ParseConfig([]byte("tools: {shellcheck: {config: 'missing.shellcheckrc'}}"))
	if err != nil {
		t.Fatal(err)
	}
	validScope := &scriptScope{paths: runPaths{analysis: root, workspace: root}}
	invalidScope := &scriptScope{paths: validScope.paths, config: invalid, composite: true}
	pos := &Pos{Line: 1, Col: 1}
	run := &ExecRun{Run: &String{Value: "echo test", Pos: pos}, RunPos: pos}
	makeScript := func(scope *scriptScope) scriptUnit {
		return scriptUnit{scope: scope, run: run, execution: scriptExecution{shell: shellcheckShell{name: "bash {0}"}, directory: runDirectory{directoryKnown, root}}}
	}
	checker := &shellcheckAnalyzer{cmd: &externalCommand{runner: func([]string, string) ([]byte, error) {
		return []byte(`{"comments":[{"line":1,"endLine":1,"column":1,"endColumn":2,"code":9999,"level":"warning","message":"retained"}]}`), nil
	}}}
	findings, err := checker.AnalyzeScripts(t.Context(), scriptBatch{scopes: []*scriptScope{validScope, invalidScope}, scripts: []scriptUnit{makeScript(validScope), makeScript(invalidScope)}})
	if err == nil || len(findings) != 1 {
		t.Fatalf("partial result lost: findings=%+v err=%v", findings, err)
	}
}
