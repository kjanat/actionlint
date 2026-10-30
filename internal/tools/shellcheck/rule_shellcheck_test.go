package shellcheck

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	shellprofile "actionlint.kjanat.dev/v2/internal/tools/profile/shell"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

func TestShellcheckInvocationDebugContext(t *testing.T) {
	command := shellcheckForTest(t)
	t.Setenv("SHELLCHECK_OPTS", "--shell=sh")
	for _, kind := range []directoryKind{directoryKnown, directoryUnknown} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			proc := analyzerhost.NewProcessPool(t.Context(), 1)
			cmd, err := proc.ConfiguredCommandRunner(command, nil, false)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			rule := newShellcheckRunForTest(cmd)
			rule.debug = func(format string, args ...any) { fmt.Fprintf(&output, format, args...) }
			rule.rcArgs = []string{"--norc"}
			rule.cmd.Args = []string{"--severity=warning"}
			rule.cmd.Env = []string{"SHELLCHECK_OPTS=--shell=bash"}
			rule.inlineConfig = &ShellcheckConfig{Enable: []string{"quote-safe-variables"}}
			directory := runDirectory{kind, t.TempDir()}
			if err := rule.runShellcheck("echo hello", workflowdecl.ScriptSourceView{}, shellprofile.Shell{Name: "sh"}, directory, &workflowlang.Pos{Line: 7, Col: 1}, nil); err != nil {
				t.Fatal(err)
			}
			if err := rule.cmd.Wait(); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				`dialect="bash" (explicit flags)`,
				`"--severity=warning" "--norc"`,
				`Inherited SHELLCHECK_OPTS="--shell=bash"`,
				`# shellcheck enable=quote-safe-variables`,
				fmt.Sprintf("%q", directory.path),
			} {
				if !strings.Contains(output.String(), want) {
					t.Errorf("missing %q in debug output:\n%s", want, output.String())
				}
			}
			disabled := strings.Contains(output.String(), "Source following disabled: working directory cannot be resolved locally")
			if disabled != (kind == directoryUnknown) {
				t.Fatalf("wrong source-following context:\n%s", output.String())
			}
		})
	}
}
