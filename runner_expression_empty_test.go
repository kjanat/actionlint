package actionlint

import (
	"io"
	"strings"
	"testing"
)

func TestRunnerExpressionNonemptyLabels(t *testing.T) {
	for _, tc := range []struct {
		name, value, kind string
	}{
		{"top-level empty", "${{ fromJSON('[]') }}", "expression"},
		{"empty labels", `${{ fromJSON('{"labels":[]}') }}`, "expression"},
		{"empty labels with group", `${{ fromJSON('{"group":"pool","labels":[]}') }}`, "expression"},
		{"mixed case empty labels", `${{ fromJSON('{"Labels":[]}') }}`, "expression"},
		{"labels field", `{labels: "${{ fromJSON('[]') }}"}`, "expression"},
		{"empty scalar retains its diagnostic", `{labels: "${{ '' }}"}`, "runner-label"},
		{"nonempty", `${{ fromJSON('["ubuntu-latest"]') }}`, ""},
		{"nonempty object", `${{ fromJSON('{"labels":["ubuntu-latest"]}') }}`, ""},
		{"group without labels", `${{ fromJSON('{"group":"pool"}') }}`, ""},
		{"unknown runner", "${{ fromJSON(vars.RUNNER) }}", ""},
		{"unknown labels", `{labels: "${{ fromJSON(vars.LABELS) }}"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			linter, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: "", Pyflakes: ""})
			if err != nil {
				t.Fatal(err)
			}
			source := "on: push\njobs:\n  build:\n    runs-on: " + tc.value + "\n    steps:\n      - run: echo ok\n"
			errs, err := linter.Lint("workflow.yml", []byte(source), nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.kind != "" {
				if len(errs) != 1 || errs[0].Kind != tc.kind {
					t.Fatalf("want one nonempty-labels diagnostic, got %v", errs)
				}
				if tc.kind == "expression" && !strings.Contains(errs[0].Message, "must contain at least one") {
					t.Fatalf("unexpected diagnostic: %v", errs[0])
				}
			} else if len(errs) != 0 {
				t.Fatalf("valid or unknown labels rejected: %v", errs)
			}
		})
	}
}
