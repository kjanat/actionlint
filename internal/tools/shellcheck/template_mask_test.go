package shellcheck

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	shellprofile "actionlint.kjanat.dev/v2/internal/tools/profile/shell"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

// A closing marker inside a template's string literal does not end the
// template: ShellCheck receives the whole template masked, never its tail.
func TestTemplateMaskUsesExpressionBoundaries(t *testing.T) {
	const script = "echo ${{ '}}' }}\n"
	pool := analyzerhost.NewProcessPool(t.Context(), 1)
	var stdin string
	cmd := pool.EmbeddedCommand("shellcheck", func(_ []string, source string) ([]byte, error) {
		stdin = source
		return []byte(`{"comments":[]}`), nil
	})
	run := newShellcheckRunForTest(cmd)
	if err := run.runShellcheck(script, workflowdecl.ScriptSourceView{}, shellprofile.Shell{Name: "bash {0}"}, runDirectory{directoryKnown, t.TempDir()}, &workflowlang.Pos{Line: 1, Col: 1}, nil); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(stdin, "echo ___________\n") || strings.ContainsAny(stdin, "'}") {
		t.Fatalf("template tail reached ShellCheck: %q", stdin)
	}

	t.Run("shellcheck", func(t *testing.T) {
		command := shellcheckForTest(t)
		proc := analyzerhost.NewProcessPool(t.Context(), 1)
		cmd, err := proc.ConfiguredCommandRunner(command, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		run := newShellcheckRunForTest(cmd)
		if err := run.runShellcheck(script, workflowdecl.ScriptSourceView{}, shellprofile.Shell{Name: "bash {0}"}, runDirectory{directoryKnown, t.TempDir()}, &workflowlang.Pos{Line: 1, Col: 1}, nil); err != nil {
			t.Fatal(err)
		}
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
		if len(run.errs) != 0 {
			t.Fatalf("masked template produced findings: %v", run.errs[0].Message)
		}
	})
}
