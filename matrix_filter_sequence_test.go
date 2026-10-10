package actionlint

import (
	"fmt"
	"strings"
	"testing"
)

func TestMatrixExcludeSequenceInsertions(t *testing.T) {
	for _, tc := range []struct {
		name, axis, filter string
		missing            bool
	}{
		{"matching filter insertion", `[false, 0, true]`, `["${{ fromJSON('[false,0]') }}", true]`, false},
		{"matching axis insertion", `["${{ fromJSON('[false,0]') }}", true]`, `[false, 0, true]`, false},
		{"nonmatching filter insertion", `[false, 0, true]`, `["${{ fromJSON('[false,0]') }}", false]`, true},
		{"nonmatching axis insertion", `["${{ fromJSON('[false,0]') }}", true]`, `[false, 0, false]`, true},
		{"empty filter insertion", `[true]`, `["${{ fromJSON('[]') }}", true]`, false},
		{"empty axis insertion", `["${{ fromJSON('[]') }}", true]`, `[true]`, false},
		{"unknown filter tail", `[false]`, `["${{ inputs.value }}", true]`, false},
		{"unknown axis tail", `["${{ inputs.value }}", false]`, `[true]`, false},
		{"nonmatching filter prefix", `[false]`, `[true, "${{ inputs.value }}"]`, true},
		{"nonmatching axis prefix", `[false, "${{ inputs.value }}"]`, `[true]`, true},
		{"returned filter string stays data", `[false]`, `["${{ fromJSON('[\"${{ false }}\"]') }}"]`, true},
		{"returned axis string stays data", `["${{ fromJSON('[\"${{ false }}\"]') }}"]`, `[false]`, true},
		{"nested array stays nested", `[[false]]`, `["${{ fromJSON('[[false]]') }}"]`, false},
		{"numeric object index", `["${{ fromJSON('[false,0]') }}", true]`, `{'2': true}`, false},
		{"coerced object index", `["${{ fromJSON('[false,0]') }}", true]`, `{'0x2': true}`, false},
		{"nonmatching object index", `["${{ fromJSON('[false,0]') }}", true]`, `{'2': false}`, true},
		{"missing object index", `["${{ fromJSON('[false,0]') }}", true]`, `{'3': true}`, true},
		{"unknown object index", `[false, "${{ inputs.value }}"]`, `{'2': true}`, false},
		{"known object index before insertion", `[false, "${{ inputs.value }}"]`, `{'0': true, '2': true}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := fmt.Sprintf("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        value: [{nested: %s}]\n        exclude: [{value: {nested: %s}}]\n    steps:\n      - run: echo ok\n", tc.axis, tc.filter)
			workflow, parseErrors := Parse([]byte(source))
			if len(parseErrors) != 0 {
				t.Fatal(parseErrors)
			}
			rule := NewRuleMatrix()
			if err := rule.VisitJobPre(workflow.Jobs["test"]); err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, finding := range rule.Errs() {
				if strings.Contains(finding.Message, "does not match in matrix") {
					count++
					if finding.Line != 8 {
						t.Fatalf("wrong exclusion position: %+v", finding)
					}
				}
			}
			want := 0
			if tc.missing {
				want = 1
			}
			if count != want {
				t.Fatalf("got %d findings, want %d: %v", count, want, rule.Errs())
			}
		})
	}
}
