package actionlint

import (
	"strings"
	"testing"
)

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
			workflow, errs := Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: {" + tc.matrix + "}\n    steps:\n      - run: echo ok\n"))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := NewRuleExpression(nil, nil)
			ty := rule.checkMatrix(workflow.Jobs["test"].Strategy.Matrix)
			if len(rule.Errs()) != 0 {
				t.Fatal(rule.Errs())
			}
			if got := ty.Props["item"].String(); got != tc.want {
				t.Fatalf("want %s, got %s", tc.want, got)
			}
		})
	}
}

func TestMatrixFilterTypePolicy(t *testing.T) {
	for _, tc := range []struct {
		matrix string
		warn   bool
	}{
		{`value: ["0"], exclude: [{value: false}]`, true},
		{`value: [false], exclude: [{value: "0"}]`, true},
		{`value: ["0x80000000"], include: [{value: 0}]`, true},
		{`value: "${{ fromJSON(vars.AXIS) }}", exclude: [{value: false}]`, true},
		{`value: "${{ fromJSON('[\"0\"]') }}", exclude: [{value: false}]`, true},
		{`value: [{nested: "0"}], exclude: [{value: {nested: false}}]`, true},
		{`value: [false], exclude: [{value: false}]`, false},
		{`value: [1], exclude: [{value: 1.0}]`, false},
		{`value: [linux], exclude: [{value: LINUX}]`, false},
		{`value: "${{ fromJSON('[false]') }}", exclude: [{value: false}]`, false},
		{`value: "${{ fromJSON(vars.AXIS) }}", exclude: [{value: linux}]`, false},
		{`value: [linux], include: [{extra: false}]`, false},
	} {
		for _, enabled := range []string{"true", "false", "null"} {
			t.Run(tc.matrix+"/"+enabled, func(t *testing.T) {
				workflow, errs := Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: {" + tc.matrix + "}\n    steps:\n      - run: echo ok\n"))
				if len(errs) != 0 {
					t.Fatal(errs)
				}
				cfg, err := ParseConfig([]byte("policy: {mixed-type-matrix-filters: " + enabled + "}"))
				if err != nil {
					t.Fatal(err)
				}
				rule := NewRuleMatrix()
				rule.SetConfig(cfg)
				if err := rule.VisitJobPre(workflow.Jobs["test"]); err != nil {
					t.Fatal(err)
				}
				count := 0
				for _, err := range rule.Errs() {
					if strings.Contains(err.Message, "policy: mixed-type-matrix-filters") {
						count++
					}
				}
				if (count == 1) != (tc.warn && enabled == "true") {
					t.Fatal(rule.Errs())
				}
			})
		}
	}
}
