package scripts_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/interpreters"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
)

func TestErrexitStatusReads(t *testing.T) {
	t.Parallel()
	// Bash's set builtin documents the exceptions for conditions, AND/OR lists,
	// negation and pipelines: https://www.gnu.org/software/bash/manual/html_node/The-Set-Builtin.html.
	for _, tc := range []struct {
		name, shell, text string
		want              int
	}{
		{"assignment", "bash", `CHANGED="$(git diff --exit-code)"; EXIT_CODE=$?`, 1},
		{"guarded", "bash", `CHANGED="$(git diff --exit-code)" || EXIT_CODE=$?`, 0},
		{"and", "bash", `false && echo ok; status=$?`, 0},
		{"disabled", "bash", `set +e; false; status=$?`, 0},
		{"restored", "bash", `set +e; set -e; false; status=$?`, 1},
		{"named option", "bash", `set +o errexit; false; status=$?`, 0},
		{"custom disabled", "bash +e {0}", `false; status=$?`, 0},
		{"custom enabled", "bash -e {0}", `false; status=$?`, 1},
		{"unknown option", "bash --mystery {0}", `false; status=$?`, 0},
		{"unknown executable", "mybash -e {0}", `false; status=$?`, 0},
		{"sh is distinct", "sh", `false; status=$?`, 0},
		{"trap", "bash", `trap 'status=$?; echo "$status"' ERR; false`, 0},
		{"condition", "bash", `if false; status=$?; then :; fi`, 0},
		{"compound", "bash", `if false; then :; fi; status=$?`, 0},
		{"subshell", "bash", `(set +e; false; status=$?); false; outer=$?`, 1},
		{"group persists", "bash", `{ set +e; }; false; status=$?`, 0},
		{"ignored group", "bash", `{ false; status=$?; } || :`, 0},
		{"ignored subshell", "bash", `(false; status=$?) || :`, 0},
		{"function condition unknown", "bash", `f() { false; status=$?; }; f || :`, 0},
		{"substitution default", "bash", `x=$(false; status=$?); echo ok`, 0},
		{"quoted", "bash", `false; echo '$?'`, 0},
		{"comment", "bash", "false\n# $?\ntrue", 0},
		{"unknown source", "bash", `. options.sh; false; status=$?`, 0},
		{"uncertain branch", "bash", `if test -e foo; then set +e; fi; false; status=$?`, 0},
		{"loop options", "bash", `for x in a b; do false; status=$?; set +e; done`, 0},
		{"noexec", "bash -n -e {0}", `false; status=$?`, 0},
		{"background options isolated", "bash", `set +e & false; status=$?`, 1},
		{"unknown function effects", "bash", `f() { alias set=true; }; f; set -e; false; status=$?`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inv := interpreters.Builtin().Analyze(tc.shell, false)
			got := scripts.ErrexitStatusReads(tc.text, inv, interpreters.Builtin())
			if len(got) != tc.want {
				t.Fatalf("got %+v, want %d", got, tc.want)
			}
			for _, span := range got {
				if !strings.HasPrefix(tc.text[span.Start:span.End], "$") {
					t.Fatalf("not a parameter range: %+v", span)
				}
			}
		})
	}
}
