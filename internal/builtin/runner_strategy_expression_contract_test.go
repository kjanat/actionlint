package builtin_test

import (
	"testing"

	"actionlint.kjanat.dev/internal/builtin"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func TestRunnerMatrixIncludeExcludeParity(t *testing.T) {
	for _, tc := range []struct {
		name, matrix string
		want         int
	}{
		{"include only", `{"include":[{"os":"ubuntu-lates"}]}`, 1},
		{"axis and include", `{"os":["ubuntu-lates"],"include":[{"os":"windows-lates"}]}`, 2},
		{"exclude introduces no labels", `{"os":["ubuntu-latest"],"exclude":[{"os":"windows-lates"}]}`, 0},
		{"declarations checked before exclusions", `{"os":["ubuntu-lates"],"exclude":[{"os":"ubuntu-lates"}]}`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var messages []string
			for _, strategy := range []string{
				"strategy:\n      matrix: " + tc.matrix,
				"strategy: ${{ fromJSON('{\"matrix\":" + tc.matrix + "}') }}",
			} {
				source := `on: push
jobs:
  test:
    ` + strategy + `
    runs-on: ${{ matrix.os }}
    steps:
      - run: echo ok
`
				workflow, parseErrors := workflowlang.ParseDocument([]byte(source))
				if len(parseErrors) != 0 {
					t.Fatal(parseErrors)
				}
				rule := builtin.NewRuleRunnerLabel()
				if err := rule.VisitJobPre(workflow.Jobs["test"]); err != nil {
					t.Fatal(err)
				}
				errs := rule.Errs()
				if len(errs) != tc.want {
					t.Fatalf("wanted %d runner-label diagnostics, got %v", tc.want, errs)
				}
				if messages == nil {
					messages = make([]string, len(errs))
					for i, err := range errs {
						messages[i] = err.Message
					}
				} else {
					for i, err := range errs {
						if err.Message != messages[i] {
							t.Fatalf("literal/expression mismatch: %q != %q", err.Message, messages[i])
						}
					}
				}
			}
		})
	}
}
