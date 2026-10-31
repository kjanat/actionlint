package expressions

import (
	"testing"
)

func TestDynamicMatrixRowsKeepInferredElementTypes(t *testing.T) {
	for _, tc := range []struct {
		name, matrix, script, wantType string
		unsafe                         bool
	}{
		{"numeric array expression argument", `item: "${{ github.event.workflow_run.pull_requests.*.number }}"`, `echo '${{ matrix.item }}'`, "number", false},
		{"numeric sequence argument", `item: ["${{ github.event.workflow_run.id }}"]`, `echo '${{ matrix.item }}'`, "number", false},
		{"numeric expression command", `item: "${{ github.event.workflow_run.pull_requests.*.number }}"`, `${{ matrix.item }}`, "number", true},
		{"string sequence remains unsafe", `item: ["${{ github.head_ref }}"]`, `echo '${{ matrix.item }}'`, "string", true},
		{"numeric includes merge", `item: "${{ github.event.workflow_run.pull_requests.*.number }}", include: [{item: 42}]`, `echo '${{ matrix.item }}'`, "number", false},
		{"string includes merge", `item: "${{ github.event.workflow_run.pull_requests.*.number }}", include: [{item: 'bad"value'}]`, `echo '${{ matrix.item }}'`, "string", true},
		{"unknown include does not inherit row type", `item: "${{ github.event.workflow_run.pull_requests.*.number }}", include: [{item: "${{ github.head_ref }}"}]`, `echo '${{ matrix.item }}'`, "any", true},
		{"unknown include array does not inherit row type", `item: "${{ github.event.workflow_run.pull_requests.*.number }}", include: "${{ fromJSON(vars.INCLUDE) }}"`, `echo '${{ matrix.item }}'`, "any", true},
		{"unknown excludes preserve numeric row type", `item: "${{ github.event.workflow_run.pull_requests.*.number }}", exclude: "${{ fromJSON(vars.EXCLUDE) }}"`, `echo '${{ matrix.item }}'`, "number", false},
		{"unknown includes can add string rows", `item: [42], include: "${{ fromJSON(vars.INCLUDE) }}"`, `echo '${{ matrix.item }}'`, "any", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "on: {workflow_run: {workflows: [Build]}}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: {" + tc.matrix + "}\n    steps:\n      - run: " + tc.script + "\n"
			rule, workflow := newTestCheckerForSource(t, source)
			job := testJob(t, workflow, "test")
			for _, err := range []error{rule.VisitWorkflowPre(workflow), rule.VisitJobPre(job)} {
				if err != nil {
					t.Fatal(err)
				}
			}
			if ty, ok := rule.matrixTy.Property("item"); !ok || ty.String() != tc.wantType {
				t.Fatalf("matrix item type=%v, want %s", ty, tc.wantType)
			}
			if err := rule.VisitStep(testStep(t, job, 0)); err != nil {
				t.Fatal(err)
			}
			unsafe := false
			for _, finding := range rule.Findings() {
				unsafe = unsafe || finding.Rule == "script-interpolation"
			}
			if unsafe != tc.unsafe {
				t.Fatalf("unsafe=%t, want %t: %v", unsafe, tc.unsafe, rule.Findings())
			}
		})
	}
}

// A matrix whose rows all come from an include expression has properties that
// are only known at runtime.
func TestIncludeExpressionMatrixAcceptsAnyProperty(t *testing.T) {
	source := `on: {workflow_call: {inputs: {targets: {type: string, required: true}}}}
jobs:
  test:
    runs-on: ${{ matrix.runner }}
    strategy:
      matrix:
        include: ${{ fromJSON(inputs.targets) }}
    steps:
      - run: echo ${{ matrix.target }}
`
	rule, workflow := newTestCheckerForSource(t, source)
	job := testJob(t, workflow, "test")
	for _, err := range []error{rule.VisitWorkflowPre(workflow), rule.VisitJobPre(job), rule.VisitStep(testStep(t, job, 0))} {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, finding := range rule.Findings() {
		if finding.Rule == "expression-reference" {
			t.Fatalf("include expression rows reported as undefined: %v", finding)
		}
	}
}
