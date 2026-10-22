package shellcheck

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/tools"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestActionInputCannotOptIntoShellcheckWithDirective(t *testing.T) {
	calls := 0
	pool := analyzerhost.NewProcessPool(t.Context(), 1)
	checker := &shellcheckAnalyzer{cmd: pool.EmbeddedCommand("shellcheck", func(_ []string, source string) ([]byte, error) {
		calls++
		if strings.Contains(source, "shellcheck shell=bash") {
			t.Error("action input reached ShellCheck through a shell directive")
		}
		before, _, _ := strings.Cut(source, "echo $x")
		line := strings.Count(before, "\n") + 1
		return fmt.Appendf(nil, `{"comments":[{"line":%d,"endLine":%d,"column":1,"endColumn":2,"code":9999,"level":"warning","message":"run finding"}]}`, line, line), nil
	})}
	pos := &workflowlang.Pos{Line: 1, Col: 1}
	scope := &tools.Scope{AnalysisDir: t.TempDir()}
	findings, err := (runner{check: checker}).Analyze(t.Context(), tools.Batch{Scopes: []*tools.Scope{scope}, Scripts: []tools.Script{
		{Input: &analyzer.ScriptInput{Action: "actions/github-script", Input: "script", Language: "javascript"}, Text: "# shellcheck shell=bash\necho $x", Key: pos, Value: pos, Scope: scope},
		{Text: "echo $x", Key: pos, Value: pos, Scope: scope, Execution: tools.Execution{Shell: "bash"}},
	}})
	if err != nil || calls != 1 || len(findings) != 1 || findings[0].Script != 1 {
		t.Fatalf("action-input isolation or original script index lost: calls=%d findings=%+v err=%v", calls, findings, err)
	}
}
