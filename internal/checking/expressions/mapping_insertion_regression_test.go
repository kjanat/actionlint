package expressions

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/model/workflows/matrix"
)

func TestUnknownMatrixKeysPreserveKnownTypes(t *testing.T) {
	t.Parallel()
	for depth := range 3 {
		t.Run(fmt.Sprintf("depth=%d", depth), func(t *testing.T) {
			t.Parallel()
			matrixValue := `{'${{ vars.AXIS }}': [ubuntu-latest], count: [1]}`
			for range depth {
				matrixValue = "{'${{ insert }}': " + matrixValue + "}"
			}
			source := "on: push\njobs:\n  test:\n    runs-on: ${{ matrix.os }}\n    strategy:\n      matrix: " + matrixValue + "\n    steps: [{run: echo ok}]\n"
			workflow, errs := parser.ParseWorkflow([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := newTestChecker()
			ty := rule.checkMatrix(ownedMatrixFixture(workflow.Jobs["test"].Strategy.Matrix), matrix.Resolve(workflow.Jobs["test"].Strategy))
			if !ty.IsLoose() || ty.Len() != 1 {
				t.Fatalf("want open object with only the known axis, got %v", ty)
			}
			if count, ok := ty.Property("count"); !ok || count.String() != "number" {
				t.Fatalf("known axis type lost: %v", ty)
			}
		})
	}
}

func TestKnownMatrixKeysStayStrict(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"os", "${{ 'os' }}", "${{ format('{0}', 'os') }}"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: " + fmt.Sprintf("{'${{ insert }}': {%q: [ubuntu-latest]}}", key) + "\n    steps: [{run: echo ok}]\n"
			workflow, errs := parser.ParseWorkflow([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := newTestChecker()
			for _, job := range workflow.Jobs {
				ty := rule.checkMatrix(ownedMatrixFixture(job.Strategy.Matrix), matrix.Resolve(job.Strategy))
				if _, ok := ty.Property("os"); !ty.IsStrict() || ty.Len() != 1 || !ok {
					t.Fatalf("known key lost or object loosened: %v", ty)
				}
			}
		})
	}
	// Preserve expression delimiters in evaluated keys as literal data.
	const key = "${{ vars.AXIS }}"
	strategy := &definition.Strategy{Matrix: &definition.Matrix{Expression: &definition.String{
		Value: `${{ fromJSON('{"${{ vars.AXIS }}":["ubuntu-latest"]}') }}`,
		Pos:   &sourcetext.Position{Line: 1, Column: 1},
	}}}
	resolution := matrix.Resolve(strategy)
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
