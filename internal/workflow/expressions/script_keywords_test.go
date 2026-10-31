package expressions

import (
	"fmt"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func TestReservedMatrixWordChangesShellGrammar(t *testing.T) {
	for _, dialect := range []syntax.LangVariant{syntax.LangBash, syntax.LangPOSIX} {
		for _, word := range []string{"echo", "fi"} {
			source := "if true; then\n  " + word + "\nfi\n"
			_, err := syntax.NewParser(syntax.Variant(dialect)).Parse(strings.NewReader(source), "")
			if invalid := err != nil; invalid != (word == "fi") {
				t.Fatalf("%s/%s: unexpected shell grammar result: %v", dialect, word, err)
			}
		}
	}
}

func TestScriptKeywordNeedsDataArgument(t *testing.T) {
	for _, shell := range []string{"bash", "sh"} {
		for _, tc := range []struct {
			name, expression, matrix, env, script string
			unsafe                                bool
		}{
			{name: "matrix keyword", expression: "matrix.word", matrix: "{word: [echo, fi]}", unsafe: true},
			{name: "matrix keyword first", expression: "matrix.word", matrix: "{word: [fi, echo]}", unsafe: true},
			{name: "matrix include", expression: "matrix.word", matrix: "{word: [echo], include: [{word: fi}]}", unsafe: true},
			{name: "nested missing member", expression: "matrix.item.word", matrix: "{item: [{word: fi}, {}]}", unsafe: true},
			{name: "nested unknown index", expression: "matrix.item[matrix.index]", matrix: "{item: [[echo, fi]], index: [0, 1]}", unsafe: true},
			{name: "environment keyword", expression: "env.WORD", env: "{WORD: fi}", unsafe: true},
			{name: "repository owner may be keyword", expression: "github.repository_owner", unsafe: true},
			{name: "constant keyword", expression: "'fi'", unsafe: true},
			{name: "elif keyword", expression: "'elif'", unsafe: true},
			{name: "Bash time keyword", expression: "'time'", unsafe: true},
			{name: "case alternative", expression: "case(github.ref == 'x', 'fi', 'echo')", unsafe: true},
			{name: "logical alternative", expression: "github.ref == 'x' && 'fi' || 'echo'", unsafe: true},
			{name: "ordinary matrix word", expression: "matrix.word", matrix: "{word: [echo, printf]}"},
			{name: "ordinary environment word", expression: "env.WORD", env: "{WORD: echo}"},
			{name: "ordinary constant word", expression: "'echo'"},
			{name: "keyword substring", expression: "'fi-linux'"},
			{name: "repository retains word proof", expression: "github.repository"},
			{name: "runner OS retains word proof", expression: "runner.os"},
			{name: "repository owner data argument", expression: "github.repository_owner", script: "echo '%s'"},
			{name: "quoted matrix argument", expression: "matrix.word", matrix: "{word: [echo, fi]}", script: "echo '%s'"},
			{name: "bare matrix argument", expression: "matrix.word", matrix: "{word: [echo, fi]}", script: "echo %s"},
			{name: "printf environment argument", expression: "env.WORD", env: "{WORD: fi}", script: "printf '%%s' '%s'"},
			{name: "quoted constant argument", expression: "'fi'", script: "echo '%s'"},
			{name: "quoted case argument", expression: "case(github.ref == 'x', 'fi', 'echo')", script: "echo '%s'"},
			{name: "unsafe text in argument", expression: "matrix.word", matrix: "{word: [echo, 'bad value']}", script: "echo '%s'", unsafe: true},
		} {
			t.Run(shell+"/"+tc.name, func(t *testing.T) {
				expression := "${{ " + tc.expression + " }}"
				script := "if true; then\n  " + expression + "\nfi"
				if tc.script != "" {
					script = fmt.Sprintf(tc.script, expression)
				}
				source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n"
				if tc.matrix != "" {
					source += "    strategy:\n      matrix: " + tc.matrix + "\n"
				}
				source += "    steps:\n      - "
				if tc.env != "" {
					source += "env: " + tc.env + "\n        "
				}
				source += "shell: " + shell + "\n        run: |\n          " + strings.ReplaceAll(script, "\n", "\n          ") + "\n"
				rule, workflow := newTestCheckerForSource(t, source)
				job := testJob(t, workflow, "test")
				for _, err := range []error{rule.VisitWorkflowPre(workflow), rule.VisitJobPre(job), rule.VisitStep(testStep(t, job, 0)), rule.VisitJobPost(job)} {
					if err != nil {
						t.Fatal(err)
					}
				}
				want := 0
				if tc.unsafe {
					want = 1
				}
				findings := rule.Findings()
				if len(findings) != want || want == 1 && (findings[0].Rule != "script-interpolation" || findings[0].Code != "unknown-script-value") {
					t.Fatalf("want %d interpolation findings, got %+v", want, findings)
				}
			})
		}
	}
}
