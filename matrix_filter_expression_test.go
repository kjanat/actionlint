package actionlint

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestMatrixExpressionFilterTypes(t *testing.T) {
	for _, tc := range []struct {
		name, axis, expression string
		warn                   bool
	}{
		{"string boolean", `["0"]`, `fromJSON('[{"value":false}]')`, true},
		{"boolean number", `[false]`, `fromJSON('[{"value":0}]')`, true},
		{"nested scalar", `[{nested: false}]`, `fromJSON('[{"value":{"nested":0}}]')`, true},
		{"matching strings", `["0"]`, `fromJSON('[{"value":"0"}]')`, false},
		{"matching numbers", `[1]`, `fromJSON('[{"value":1.0}]')`, false},
		{"expression axis", `"${{ fromJSON('[false]') }}"`, `fromJSON('[{"value":0}]')`, true},
		{"new property", `[false]`, `fromJSON('[{"extra":0}]')`, false},
		{"empty filters", `[false]`, `fromJSON('[]')`, false},
		{"dynamic", `[false]`, `fromJSON(vars.FILTERS)`, false},
		{"invalid root", `[false]`, `fromJSON('{"value":0}')`, false},
		{"invalid element", `[false]`, `fromJSON('[0]')`, false},
		{"invalid mixed elements", `[false]`, `fromJSON('[{"value":0},null]')`, false},
		{"invalid JSON", `[false]`, `fromJSON('[{"value":0}] trailing')`, false},
		{"ambiguous JSON", `[false]`, `fromJSON('[{"value":0,"VALUE":false}]')`, false},
		{"returned template stays data", `[false]`, `fromJSON('[{"value":"${{ false }}"}]')`, true},
	} {
		for _, section := range []string{"include", "exclude"} {
			for _, enabled := range []string{"true", "false"} {
				t.Run(tc.name+"/"+section+"/"+enabled, func(t *testing.T) {
					expression := strconv.Quote("${{ " + tc.expression + " }}")
					source := fmt.Sprintf("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        value: %s\n        %s: %s\n    steps:\n      - run: echo ok\n", tc.axis, section, expression)
					workflow, parseErrors := Parse([]byte(source))
					if len(parseErrors) != 0 {
						t.Fatal(parseErrors)
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
					for _, finding := range rule.Errs() {
						if strings.Contains(finding.Message, "policy: mixed-type-matrix-filters") {
							count++
							if finding.Line != 8 || finding.Column <= 8 {
								t.Fatalf("wrong expression position: %+v", finding)
							}
						}
					}
					want := 0
					if tc.warn && enabled == "true" {
						want = 1
					}
					if count != want {
						t.Fatalf("got %d findings, want %d: %v", count, want, rule.Errs())
					}
				})
			}
		}
	}
}

func TestMatrixExpressionFilterLeafTypes(t *testing.T) {
	for _, tc := range []struct {
		name, axis, filter string
		warn               bool
	}{
		{"boolean", `["0"]`, `"${{ fromJSON('false') }}"`, true},
		{"number", `[false]`, `"${{ 0 }}"`, true},
		{"null", `[false]`, `"${{ null }}"`, true},
		{"matching boolean", `[false]`, `"${{ false }}"`, false},
		{"matching number", `[0]`, `"${{ fromJSON('0.0') }}"`, false},
		{"matching null", `[null]`, `"${{ null }}"`, false},
		{"nested object", `[{nested: false}]`, `{nested: "${{ 0 }}"}`, true},
		{"nested array", `[{nested: [false]}]`, `{nested: ["${{ 0 }}"]}`, true},
		{"object expression", `[{nested: false}]`, `"${{ fromJSON('{\"nested\":0}') }}"`, true},
		{"array expression", `[{nested: [false]}]`, `{nested: "${{ fromJSON('[0]') }}"}`, true},
		{"dynamic", `[false]`, `"${{ inputs.value }}"`, false},
		{"nested dynamic", `[{nested: false}]`, `{nested: "${{ inputs.value }}"}`, false},
		{"dynamic sibling", `[{nested: false, other: false}]`, `{nested: "${{ inputs.value }}", other: "${{ 0 }}"}`, true},
		{"returned template stays string", `[false]`, `"${{ fromJSON('\"${{ false }}\"') }}"`, true},
		{"returned object template stays string", `[{nested: false}]`, `"${{ fromJSON('{\"nested\":\"${{ false }}\"}') }}"`, true},
	} {
		for _, section := range []string{"include", "exclude"} {
			t.Run(tc.name+"/"+section, func(t *testing.T) {
				source := fmt.Sprintf("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        value: %s\n        %s: [{value: %s}]\n    steps:\n      - run: echo ok\n", tc.axis, section, tc.filter)
				workflow, parseErrors := Parse([]byte(source))
				if len(parseErrors) != 0 {
					t.Fatal(parseErrors)
				}
				cfg, err := ParseConfig([]byte("policy: {mixed-type-matrix-filters: true}"))
				if err != nil {
					t.Fatal(err)
				}
				rule := NewRuleMatrix()
				rule.SetConfig(cfg)
				if err := rule.VisitJobPre(workflow.Jobs["test"]); err != nil {
					t.Fatal(err)
				}
				count := 0
				for _, finding := range rule.Errs() {
					if strings.Contains(finding.Message, "policy: mixed-type-matrix-filters") {
						count++
						if finding.Line != 8 {
							t.Fatalf("wrong filter position: %+v", finding)
						}
					}
				}
				want := 0
				if tc.warn {
					want = 1
				}
				if count != want {
					t.Fatalf("got %d findings, want %d: %v", count, want, rule.Errs())
				}
			})
		}
	}
}
