package actionlint

import (
	"fmt"
	"strconv"
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
			for _, enabled := range []string{"on", "off"} {
				t.Run(tc.name+"/"+section+"/"+enabled, func(t *testing.T) {
					expression := strconv.Quote("${{ " + tc.expression + " }}")
					source := fmt.Sprintf("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        value: %s\n        %s: %s\n    steps:\n      - run: echo ok\n", tc.axis, section, expression)
					workflow, parseErrors := Parse([]byte(source))
					if len(parseErrors) != 0 {
						t.Fatal(parseErrors)
					}
					cfg, err := ParseConfig([]byte("lint: {rules: {suspicious: {mixed-type-matrix-filters: " + enabled + "}}}"))
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
						if finding.Kind == "mixed-type-matrix-filters" {
							count++
							if finding.Line != 8 || finding.Column <= 8 {
								t.Fatalf("wrong expression position: %+v", finding)
							}
						}
					}
					want := 0
					if tc.warn && enabled == "on" {
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
		{"matching array insertion", `[{nested: [false]}]`, `{nested: ["${{ fromJSON('[false]') }}"]}`, false},
		{"mixed array insertion", `[{nested: [false]}]`, `{nested: ["${{ fromJSON('[0]') }}"]}`, true},
		{"empty array insertion", `[{nested: [false]}]`, `{nested: ["${{ fromJSON('[]') }}", false]}`, false},
		{"array insertion shifts siblings", `[{nested: [false, 0, true]}]`, `{nested: ["${{ fromJSON('[false,0]') }}", true]}`, false},
		{"array insertion preserves sibling expressions", `[{nested: [false, true]}]`, `{nested: ["${{ fromJSON('[false]') }}", "${{ true }}"]}`, false},
		{"array insertion one level", `[{nested: [[false]]}]`, `{nested: ["${{ fromJSON('[[false]]') }}"]}`, false},
		{"dynamic array insertion", `[{nested: [false]}]`, `{nested: ["${{ inputs.value }}", 0]}`, false},
		{"known prefix before dynamic insertion", `[{nested: [false]}]`, `{nested: [0, "${{ inputs.value }}"]}`, true},
		{"decoded array insertion stays data", `[{nested: [false]}]`, `{nested: ["${{ fromJSON('[\"${{ false }}\"]') }}"]}`, true},
		{"axis array insertion", `[{nested: ["${{ fromJSON('[\"0\"]') }}"]}]`, `{nested: [false]}`, true},
		{"matching axis array insertion", `[{nested: ["${{ fromJSON('[false]') }}"]}]`, `{nested: [false]}`, false},
		{"empty axis insertion", `[{nested: ["${{ fromJSON('[]') }}", false]}]`, `{nested: [false]}`, false},
		{"axis insertion shifts siblings", `[{nested: ["${{ fromJSON('[false,0]') }}", true]}]`, `{nested: [false, 0, true]}`, false},
		{"axis insertion one level", `[{nested: ["${{ fromJSON('[[false]]') }}"]}]`, `{nested: [[false]]}`, false},
		{"axis mapping array value", `[{nested: "${{ fromJSON('[false]') }}"}]`, `{nested: [false]}`, false},
		{"unknown axis insertion", `[{nested: ["${{ inputs.value }}", 0]}]`, `{nested: [false, false]}`, false},
		{"known prefix before unknown axis", `[{nested: [false, "${{ inputs.value }}"]}]`, `{nested: [0, false]}`, true},
		{"unknown array independent property", `[{nested: [false], other: false}]`, `{nested: ["${{ inputs.value }}", 0], other: 0}`, true},
		{"decoded axis insertion stays data", `[{nested: ["${{ fromJSON('[\"${{ false }}\"]') }}"]}]`, `{nested: [false]}`, true},
		{"numeric object index after insertion", `[{nested: ["${{ fromJSON('[false,0]') }}", true]}]`, `{nested: {'2': true}}`, false},
		{"coerced object index after insertion", `[{nested: ["${{ fromJSON('[false,0]') }}", true]}]`, `{nested: {'0x2': true}}`, false},
		{"mixed object index after insertion", `[{nested: ["${{ fromJSON('[false,0]') }}", true]}]`, `{nested: {'2': 1}}`, true},
		{"missing object index after insertion", `[{nested: ["${{ fromJSON('[false,0]') }}", true]}]`, `{nested: {'3': true}}`, true},
		{"unknown object index after insertion", `[{nested: [false, "${{ inputs.value }}"]}]`, `{nested: {'2': true}}`, false},
		{"known object index before insertion", `[{nested: [false, "${{ inputs.value }}"]}]`, `{nested: {'0': 0, '2': true}}`, true},
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

func TestMatrixExpressionFilterCombinations(t *testing.T) {
	for _, tc := range []struct {
		name, axis, expression string
		count                  int
	}{
		{"array insertion", `["0"]`, `fromJSON('[{"value":false}]')`, 1},
		{"object insertion", `["0"]`, `fromJSON('{"value":false}')`, 1},
		{"multiple insertions", `["0"]`, `fromJSON('[{"value":false},{"value":0}]')`, 2},
		{"matching object", `[false]`, `fromJSON('{"value":false}')`, 0},
		{"matching array", `[false]`, `fromJSON('[{"value":false}]')`, 0},
		{"nested object", `[{nested: false}]`, `fromJSON('[{"value":{"nested":0}}]')`, 1},
		{"nested array", `[{nested: [false]}]`, `fromJSON('{"value":{"nested":[0]}}')`, 1},
		{"empty insertion", `[false]`, `fromJSON('[]')`, 0},
		{"dynamic", `[false]`, `fromJSON(inputs.filters)`, 0},
		{"scalar", `[false]`, `fromJSON('false')`, 0},
		{"nested insertion array", `[false]`, `fromJSON('[[{"value":0}]]')`, 0},
		{"mixed invalid insertion", `[false]`, `fromJSON('[{"value":0},null]')`, 0},
		{"invalid JSON", `[false]`, `fromJSON('[{"value":0}] trailing')`, 0},
		{"decoded template stays data", `[false]`, `fromJSON('[{"value":"${{ false }}"}]')`, 1},
		{"decoded nested template stays data", `[{nested: false}]`, `fromJSON('{"value":{"nested":"${{ false }}"}}')`, 1},
		{"decoded element is not executed", `[false]`, `fromJSON('["${{ fromJSON(''[{\"value\":0}]'') }}"]')`, 0},
	} {
		for _, section := range []string{"include", "exclude"} {
			t.Run(tc.name+"/"+section, func(t *testing.T) {
				expression := strconv.Quote("${{ " + tc.expression + " }}")
				source := fmt.Sprintf("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        value: %s\n        %s:\n          - %s\n          - {value: \"${{ false }}\"}\n    steps:\n      - run: echo ok\n", tc.axis, section, expression)
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
				count, siblings := 0, 0
				for _, finding := range rule.Errs() {
					if strings.Contains(finding.Message, "policy: mixed-type-matrix-filters") {
						if finding.Line == 10 {
							siblings++
							continue
						}
						count++
						if finding.Line != 9 || finding.Column != 13 {
							t.Fatalf("wrong combination position: %+v", finding)
						}
					}
				}
				if count != tc.count {
					t.Fatalf("got %d findings, want %d: %v", count, tc.count, rule.Errs())
				}
				wantSiblings := 0
				if tc.axis == `["0"]` {
					wantSiblings = 1
				}
				if siblings != wantSiblings {
					t.Fatalf("got %d sibling findings, want %d: %v", siblings, wantSiblings, rule.Errs())
				}
			})
		}
	}
}

func TestKnownMatrixExpressionFilterTypes(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		warn        bool
	}{
		{"mixed", `{"value":["0"],"exclude":[{"value":false}]}`, true},
		{"matching", `{"value":[false],"exclude":[{"value":false}]}`, false},
		{"nested", `{"value":[{"nested":false}],"include":[{"value":{"nested":0}}]}`, true},
		{"decoded filter stays data", `{"value":[false],"exclude":[{"value":"${{ false }}"}]}`, true},
		{"decoded axis stays data", `{"value":["${{ false }}"],"exclude":[{"value":false}]}`, true},
		{"invalid root", `[]`, false},
		{"invalid axis", `{"value":false,"exclude":[{"value":0}]}`, false},
		{"invalid filters", `{"value":[false],"exclude":[0]}`, false},
		{"invalid JSON", `{"value":[false]} trailing`, false},
		{"dynamic", "", false},
	} {
		for _, section := range []string{"matrix", "strategy"} {
			t.Run(tc.name+"/"+section, func(t *testing.T) {
				value := tc.value
				if section == "strategy" {
					value = `{"matrix":` + value + `}`
				}
				expression := "${{ fromJSON('" + value + "') }}"
				if tc.name == "dynamic" {
					expression = "${{ fromJSON(inputs.value) }}"
				}
				strategy := "    strategy: " + strconv.Quote(expression)
				line, column := 5, 15
				if section == "matrix" {
					strategy = "    strategy:\n      matrix: " + strconv.Quote(expression)
					line, column = 6, 15
				}
				source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n" + strategy + "\n    steps:\n      - run: echo ok\n"
				workflow, parseErrors := Parse([]byte(source))
				if len(parseErrors) != 0 {
					t.Fatal(parseErrors)
				}
				job := workflow.Jobs["test"]
				originalMatrix := job.Strategy.Matrix
				cfg, err := ParseConfig([]byte("policy: {mixed-type-matrix-filters: true}"))
				if err != nil {
					t.Fatal(err)
				}
				rule := NewRuleMatrix()
				rule.SetConfig(cfg)
				if err := rule.VisitJobPre(job); err != nil {
					t.Fatal(err)
				}
				if job.Strategy.Matrix != originalMatrix {
					t.Fatal("matrix AST was replaced during analysis")
				}
				count := 0
				for _, finding := range rule.Errs() {
					if strings.Contains(finding.Message, "policy: mixed-type-matrix-filters") {
						count++
						if finding.Line != line || finding.Column != column {
							t.Fatalf("wrong matrix position: %+v", finding)
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
