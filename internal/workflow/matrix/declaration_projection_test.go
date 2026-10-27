package matrix

import (
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestDeclarationCombinationsKeepValidationAndExecutionSeparate(t *testing.T) {
	for _, tc := range []struct {
		name, matrix       string
		declared, executed int
		executionComplete  bool
	}{
		{"excluded typo", `{"os":["ubuntu-lates"],"exclude":[{"os":"ubuntu-lates"}]}`, 1, 0, true},
		{"include correlations", `{"os":["linux"],"exclude":[{"os":"linux"}],"include":[{"os":"linux","image":"first"},{"os":"linux","extra":"second"}]}`, 3, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			strategies := []string{"strategy:\n      matrix: " + tc.matrix, "strategy: ${{ fromJSON('{\"matrix\":" + tc.matrix + "}') }}"}
			for _, strategy := range strategies {
				workflow, findings := workflowlang.Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    " + strategy + "\n    steps: [{run: echo ok}]\n"))
				if len(findings) != 0 {
					t.Fatal(findings)
				}
				resolution := Resolve(workflow.Jobs["test"].Strategy)
				for range 2 {
					declared, complete := resolution.DeclarationCombinations(16)
					if !complete || len(declared) != tc.declared {
						t.Fatalf("declaration projection: count=%d complete=%t", len(declared), complete)
					}
					if tc.name == "include correlations" {
						_, firstImage := declared[0].Get("image")
						_, firstExtra := declared[0].Get("extra")
						_, secondImage := declared[1].Get("image")
						_, secondExtra := declared[1].Get("extra")
						if !firstImage || firstExtra || secondImage || !secondExtra {
							t.Fatal("standalone include assignments were combined")
						}
					}
					executed, complete := resolution.Combinations(16)
					if complete != tc.executionComplete || len(executed) != tc.executed {
						t.Fatalf("execution projection changed: count=%d complete=%t", len(executed), complete)
					}
				}
			}
		})
	}
}
