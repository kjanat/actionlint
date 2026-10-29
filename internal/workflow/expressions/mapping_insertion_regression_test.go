package expressions

import (
	"fmt"
	"testing"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	matrixcheck "actionlint.kjanat.dev/v2/internal/workflow/matrix"
)

func TestUnknownMatrixKeysPreserveKnownTypes(t *testing.T) {
	for depth := range 3 {
		t.Run(fmt.Sprintf("depth=%d", depth), func(t *testing.T) {
			matrix := `{'${{ vars.AXIS }}': [ubuntu-latest], count: [1]}`
			for range depth {
				matrix = "{'${{ insert }}': " + matrix + "}"
			}
			source := "on: push\njobs:\n  test:\n    runs-on: ${{ matrix.os }}\n    strategy:\n      matrix: " + matrix + "\n    steps: [{run: echo ok}]\n"
			workflow, errs := workflowparser.ParseDocument([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := newTestChecker()
			ty := rule.checkMatrix(ownedMatrixFixture(workflow.Jobs["test"].Strategy.Matrix), matrixcheck.Resolve(workflow.Jobs["test"].Strategy))
			if !ty.IsLoose() || len(ty.Props) != 1 {
				t.Fatalf("want open object with only the known axis, got %#v", ty)
			}
			if count, ok := ty.Props["count"]; !ok || count.String() != "number" {
				t.Fatalf("known axis type lost: %#v", ty.Props)
			}
		})
	}
}

func TestKnownMatrixKeysStayStrict(t *testing.T) {
	for _, key := range []string{"os", "${{ 'os' }}", "${{ format('{0}', 'os') }}"} {
		t.Run(key, func(t *testing.T) {
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: " + fmt.Sprintf("{'${{ insert }}': {%q: [ubuntu-latest]}}", key) + "\n    steps: [{run: echo ok}]\n"
			workflow, errs := workflowparser.ParseDocument([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := newTestChecker()
			for _, job := range workflow.Jobs {
				ty := rule.checkMatrix(ownedMatrixFixture(job.Strategy.Matrix), matrixcheck.Resolve(job.Strategy))
				if !ty.IsStrict() || len(ty.Props) != 1 || ty.Props["os"] == nil {
					t.Fatalf("known key lost or object loosened: %#v", ty)
				}
			}
		})
	}
	// Preserve expression delimiters in evaluated keys as literal data.
	const key = "${{ vars.AXIS }}"
	strategy := &workflowlang.Strategy{Matrix: &workflowlang.Matrix{Expression: &workflowlang.String{
		Value: `${{ fromJSON('{"${{ vars.AXIS }}":["ubuntu-latest"]}') }}`,
		Pos:   &workflowlang.Pos{Line: 1, Col: 1},
	}}}
	resolution := matrixcheck.Resolve(strategy)
	if combinations, complete := resolution.Combinations(8); !complete || len(combinations) != 1 {
		t.Fatal("literal matrix rejected")
	}
	reachable := resolution.Reachable(key)
	if !reachable.Complete || len(reachable.Values) != 1 {
		t.Fatalf("evaluated key reinterpreted: %#v", reachable)
	}
	if got, ok := reachable.Values[0].ScalarString(); !ok || got != "ubuntu-latest" {
		t.Fatalf("evaluated key value changed: %q, %v", got, ok)
	}
}
