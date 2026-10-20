package execution_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/workflow/execution"

	"mvdan.cc/sh/v3/syntax"
)

func TestLiteralShellWordQuotes(t *testing.T) {
	for _, tc := range []struct {
		word, want string
		known      bool
	}{
		{`"./{good,bad}.sh"`, "./{good,bad}.sh", true},
		{`"./*.sh"`, "./*.sh", true},
		{`"./[ab]?.sh"`, "./[ab]?.sh", true},
		{`"~/.sh"`, "~/.sh", true},
		{`./"{good,bad}".sh`, "./{good,bad}.sh", true},
		{`./{good,bad}.sh`, "", false},
		{`./*.sh`, "", false},
		{`"./$FILE"`, "", false},
		{`"./$(echo file)"`, "", false},
		{`"./\\file"`, "", false},
	} {
		t.Run(tc.word, func(t *testing.T) {
			file, err := syntax.NewParser().Parse(strings.NewReader(tc.word), "")
			if err != nil {
				t.Fatal(err)
			}
			call, ok := file.Stmts[0].Cmd.(*syntax.CallExpr)
			if !ok {
				t.Fatalf("not a call: %T", file.Stmts[0].Cmd)
			}
			got, known := execution.LiteralShellWord(call.Args[0].Parts)
			if got != tc.want || known != tc.known {
				t.Fatalf("got %q, %v; want %q, %v", got, known, tc.want, tc.known)
			}
		})
	}
}
