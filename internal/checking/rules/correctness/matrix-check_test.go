package correctness

import (
	"reflect"
	"testing"

	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestMatrixCheckUsesIndependentJobCallbacks(t *testing.T) {
	t.Parallel()
	const source = `on: push
jobs:
  duplicate:
    runs-on: ubuntu-latest
    strategy:
      matrix: {item: [1, 1]}
    steps: [{run: echo ok}]
  clean:
    runs-on: ubuntu-latest
    strategy:
      matrix: {item: [2]}
    steps: [{run: echo ok}]
  mixed:
    runs-on: ubuntu-latest
    strategy:
      matrix: {item: [1], exclude: [{item: '1'}]}
    steps: [{run: echo ok}]
`
	parsed, errors := workflowparser.Parse([]byte(source))
	if len(errors) != 0 {
		t.Fatal(errors)
	}
	workflow := declaration.Publish(source, parsed, false, "").Bind("test").Workflow()
	duplicate, _ := workflow.Job("duplicate")
	clean, _ := workflow.Job("clean")
	mixed, _ := workflow.Job("mixed")
	for _, enabled := range []bool{false, true} {
		check := MatrixCheck(enabled)
		if check.Name != "matrix" || check.Job == nil || check.Workflow != nil || check.Step != nil {
			t.Fatalf("unexpected declaration callbacks: %+v", check)
		}
		first := check.Job(duplicate)
		if len(first) != 1 || first[0].Rule != "matrix" || first[0].Start.Line != 6 {
			t.Fatalf("duplicate finding lost its rule or source: %+v", first)
		}
		if findings := check.Job(clean); len(findings) != 0 {
			t.Fatalf("previous job findings leaked: %+v", findings)
		}
		hasMixed := false
		for _, finding := range check.Job(mixed) {
			hasMixed = hasMixed || finding.Rule == "mixed-type-matrix-filters"
		}
		if hasMixed != enabled {
			t.Fatalf("mixed-type finding=%t, enabled=%t", hasMixed, enabled)
		}
		if next := check.Job(duplicate); !reflect.DeepEqual(first, next) {
			t.Fatalf("job callback depends on call order: first=%+v next=%+v", first, next)
		}
	}
}
