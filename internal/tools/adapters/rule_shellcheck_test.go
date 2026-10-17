package adapters

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/analyzerhost"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
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
			if err := rule.runShellcheck("echo hello", nil, shellcheckShell{name: "sh"}, directory, &workflowlang.Pos{Line: 7, Col: 1}, nil); err != nil {
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

func TestRuleShellcheckSanitizeExpressionsInScript(t *testing.T) {
	testCases := []struct {
		input string
		want  string
	}{
		{
			"",
			"",
		},
		{
			"foo",
			"foo",
		},
		{
			"${{}}",
			"_____",
		},
		{
			"${{ matrix.foo }}",
			"_________________",
		},
		{
			"aaa ${{ matrix.foo }} bbb",
			"aaa _________________ bbb",
		},
		{
			"${{}}${{}}",
			"__________",
		},
		{
			"p${{a}}q${{b}}r",
			"p______q______r",
		},
		{
			"${{",
			"${{",
		},
		{
			"}}",
			"}}",
		},
		{
			"aaa${{foo",
			"aaa${{foo",
		},
		{
			"a${{b}}${{c",
			"a______${{c",
		},
		{
			"a${{b}}c}}d",
			"a______c}}d",
		},
		{
			"a}}b${{c}}d",
			"a}}b______d",
		},
		{
			"before ${{\nfoo\n}} after",
			"before ___\n___\n__ after",
		},
		{
			"before ${{\né\n}} after",
			"before ___\n_\n__ after",
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d_%s", i, tc.input), func(t *testing.T) {
			have := sanitizeExpressionsInScript(tc.input)
			if tc.want != have {
				t.Fatalf(`sanitized result is unexpected.
want: %q
have: %q`, tc.want, have)
			}
		})
	}
}
