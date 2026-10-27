package expressions

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	matrixcheck "actionlint.kjanat.dev/v2/internal/workflow/matrix"
)

func TestMatrixCombinationInsertionValidation(t *testing.T) {
	for _, section := range []string{"include", "exclude"} {
		for _, tc := range []struct {
			name, expression string
			wantErrors       int
		}{
			{"objects", `fromJSON('[{"item":"one"},{"item":"two"}]')`, 0},
			{"different property types", `fromJSON('[{"item":"one"},{"item":1}]')`, 0},
			{"nested property", `fromJSON('[{"item":[1,null]}]')`, 0},
			{"empty", `fromJSON('[]')`, 0},
			{"single object", `fromJSON('{"item":"one"}')`, 0},
			{"unknown", `fromJSON(vars.MATRIX)`, 0},
			{"object then number", `fromJSON('[{"item":"one"},1]')`, 1},
			{"number then object", `fromJSON('[1,{"item":"one"}]')`, 1},
			{"object then string", `fromJSON('[{"item":"one"},"two"]')`, 1},
			{"object then boolean", `fromJSON('[{"item":"one"},false]')`, 1},
			{"object then null", `fromJSON('[{"item":"one"},null]')`, 1},
			{"object then nested array", `fromJSON('[{"item":"one"},[{"item":"two"}]]')`, 1},
			{"multiple invalid elements", `fromJSON('[{"item":"one"},1,null]')`, 2},
			{"scalar array", `fromJSON('[1]')`, 1},
			{"nested array", `fromJSON('[[{"item":"one"}]]')`, 1},
			{"scalar", `1`, 1},
		} {
			t.Run(section+"/"+tc.name, func(t *testing.T) {
				matrix := "item: [one], " + section + ": [" + strconv.Quote("${{ "+tc.expression+" }}") + "]"
				workflow, errs := workflowparser.ParseDocument([]byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix: {` + matrix + `}
    steps:
      - run: echo ok
`))
				if len(errs) != 0 {
					t.Fatal(errs)
				}
				rule := newTestChecker()
				rule.checkMatrix(ownedMatrixFixture(workflow.Jobs["test"].Strategy.Matrix), matrixcheck.Resolve(workflow.Jobs["test"].Strategy))
				if errs := rule.Findings(); len(errs) != tc.wantErrors {
					t.Fatalf("want %d errors, got %v", tc.wantErrors, errs)
				}
				for _, err := range rule.Findings() {
					if !strings.Contains(err.Message, section) || !strings.Contains(err.Message, "must be an object") {
						t.Fatalf("unexpected error: %v", err)
					}
				}
			})
		}
	}
}

func TestResolvedReservedMatrixKeyKeepsExpressionSource(t *testing.T) {
	for _, section := range []string{"include", "exclude"} {
		t.Run(section, func(t *testing.T) {
			source := fmt.Sprintf("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        axis: [one]\n        \"${{ case(true, '%s', missing) }}\": [{axis: one}]\n    steps: [{run: echo ok}]\n", section)
			workflow, errs := workflowparser.ParseDocument([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			matrix := workflow.Jobs["test"].Strategy.Matrix
			var sourceKey *workflowlang.String
			if section == "include" && matrix.Include != nil {
				sourceKey = matrix.Include.Name
			} else if section == "exclude" && matrix.Exclude != nil {
				sourceKey = matrix.Exclude.Name
			}
			if sourceKey == nil || sourceKey.Value != fmt.Sprintf("${{ case(true, '%s', missing) }}", section) {
				t.Fatalf("reserved key source lost: %#v", matrix)
			}
			rule := newTestChecker()
			rule.checkMatrix(ownedMatrixFixture(matrix), matrixcheck.Resolve(workflow.Jobs["test"].Strategy))
			if len(rule.Findings()) != 1 || !strings.Contains(rule.Findings()[0].Message, `undefined variable "missing"`) {
				t.Fatalf("want source-key expression diagnostic, got %v", rule.Findings())
			}
		})
	}
}

func TestMatrixSequenceInsertion(t *testing.T) {
	// Local SDK: 80bb1fb827fa44d489263061e71ef4adba7ad8cd, EvalStrategy,
	// next engine, loose JSON, anchors rejected. Hosted behavior is unmeasured.
	for _, tc := range []struct{ name, matrix, want string }{
		{"expression array", `item: [before, "${{ fromJSON('[\"one\",\"two\"]') }}", after]`, "string"},
		{"literal array", `item: [[one, two]]`, "array<string>"},
		{"empty insertion", `item: [before, "${{ fromJSON('[]') }}", after]`, "string"},
		{"one level", `item: ["${{ fromJSON('[[1,2],[3,4]]') }}"]`, "array<number>"},
		{"literal nested sequence", `item: [["${{ fromJSON('[1,2]') }}"]]`, "array<number>"},
		{"include insertion", `include: ["${{ fromJSON('[{\"item\":\"one\"},{\"item\":\"two\"}]') }}"]`, "string"},
		{"empty include insertion", `item: [before], include: ["${{ fromJSON('[]') }}"]`, "string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, errs := workflowparser.ParseDocument([]byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix: {` + tc.matrix + `}
    steps:
      - run: echo ok
`))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := newTestChecker()
			ty := rule.checkMatrix(ownedMatrixFixture(workflow.Jobs["test"].Strategy.Matrix), matrixcheck.Resolve(workflow.Jobs["test"].Strategy))
			if len(rule.Findings()) != 0 {
				t.Fatal(rule.Findings())
			}
			if got := ty.Props["item"].String(); got != tc.want {
				t.Fatalf("want %s, got %s", tc.want, got)
			}
		})
	}
}
