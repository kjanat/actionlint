package matrix

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

func TestRuleMatrixScalarValues(t *testing.T) {
	tests := []struct {
		name   string
		matrix string
		want   string
	}{
		{"quoted number", `{value: [1, "1"]}`, ""},
		{"quoted decimal", `{value: [3.10, "3.10"]}`, ""},
		{"equal numbers", `{value: [1, 1.0]}`, "duplicate value"},
		{"numeric spellings", `{value: [10, 1e1]}`, "duplicate value"},
		{"boolean spellings", `{value: [true, True]}`, "duplicate value"},
		{"null spellings", `{value: [null, ~]}`, "duplicate value"},
		{"quoted boolean", `{value: [true, "true"]}`, ""},
		{"quoted null", `{value: [null, "null"]}`, ""},
		{"exclude numeric string", `{value: ["1.0"], exclude: [{value: 1.0}]}`, ""},
		{"exclude same number", `{value: [1], exclude: [{value: 1.0}]}`, ""},
		{"exclude case-insensitive string", `{value: [LINUX], exclude: [{value: linux}]}`, ""},
		{"exclude bool number", `{value: [1], exclude: [{value: true}]}`, ""},
		{"exclude null zero", `{value: [0], exclude: [{value: null}]}`, ""},
		{"exclude nonnumeric string", `{value: [1], exclude: [{value: "one"}]}`, "does not match"},
		{"nested numeric string", `{value: [{version: "1"}], exclude: [{value: {version: 1}}]}`, ""},
		{"nested same number", `{value: [[1]], exclude: [{value: [1.0]}]}`, ""},
		{"nested array prefix", `{value: [[LINUX, 24]], exclude: [{value: [linux]}]}`, ""},
		{"nested missing null", `{value: [{os: linux}], exclude: [{value: {version: null}}]}`, ""},
		{"nested missing nonnull", `{value: [{os: linux}], exclude: [{value: {version: 24}}]}`, "does not match"},
		{"nested missing array index", `{value: [[linux]], exclude: [{value: [LINUX, null]}]}`, ""},
		{"empty nested filter", `{value: [{os: linux}], exclude: [{value: {}}]}`, ""},
		{"nested dynamic value", `{value: [{os: "${{ inputs.os }}"}], exclude: [{value: {os: linux}}]}`, ""},
		{"nested dynamic filter", `{value: [{os: linux}], exclude: [{value: {os: "${{ inputs.os }}"}}]}`, ""},
		{"include value", `{os: [ubuntu-latest], include: [{os: macos-latest}], exclude: [{os: macos-latest}]}`, "does not match"},
		{"include key", `{os: [ubuntu-latest], include: [{gui: gnome}], exclude: [{gui: gnome}]}`, "does not exist"},
		{"include only", `{include: [{os: macos-latest}], exclude: [{os: macos-latest}]}`, "no matrix variation"},
		{"dynamic include", `{os: [ubuntu-latest], include: "${{ fromJSON(inputs.extra) }}", exclude: [{os: macos-latest}]}`, "does not match"},
		{"dynamic include entry", `{os: [ubuntu-latest], include: ["${{ fromJSON(inputs.extra) }}"], exclude: [{os: macos-latest}]}`, "does not match"},
		{"original value", `{os: [ubuntu-latest], include: [{os: macos-latest}], exclude: [{os: ubuntu-latest}]}`, ""},
		{"dynamic row", `{os: "${{ fromJSON(inputs.systems) }}", exclude: [{os: macos-latest}]}`, ""},
		{"dynamic row value", `{os: ["${{ inputs.system }}"], exclude: [{os: macos-latest}]}`, ""},
		{"dynamic exclude", `{os: [ubuntu-latest], exclude: "${{ fromJSON(inputs.excluded) }}"}`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix: ` + tc.matrix + `
    steps:
      - run: echo test
`
			w, parseErrors := workflowlang.Parse([]byte(src))
			if len(parseErrors) != 0 {
				t.Fatal(parseErrors)
			}
			errs := Resolve(w.Jobs["test"].Strategy).Check(false)
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatal(errs)
				}
			} else if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
				t.Fatalf("wanted one %q error, got %v", tc.want, errs)
			}
		})
	}
}

func TestMatrixDiagnosticValueDisplay(t *testing.T) {
	for _, tc := range []struct{ name, expression, want string }{
		{"null", `null`, "null"},
		{"empty array", `fromJSON('[]')`, "[]"},
		{"empty object", `fromJSON('{}')`, "{}"},
		{"nested values", `fromJSON('{"z":[null,true,2,{"b":"text","a":[false]}],"a":{"x":null}}')`, `{"a": {"x": null}, "z": [null, true, 2, {"a": [false], "b": "text"}]}`},
		{"returned syntax stays data", `fromJSON('["${{ vars.TEXT }}"]')`, `["${{ vars.TEXT }}"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			semantic := value.Template("${{ "+tc.expression+" }}", value.Origin{})
			if got := display(semantic); got != tc.want {
				t.Fatalf("display=%s, want %s", got, tc.want)
			}
		})
	}
	unsorted := value.Map(value.Origin{},
		value.Prop{Name: "z", Value: value.YAMLScalar("last", "!!str", value.Origin{})},
		value.Prop{Name: "a", Value: value.Map(value.Origin{},
			value.Prop{Name: "z", Value: value.FromJSON(nil, value.Origin{})},
			value.Prop{Name: "a", Value: value.Seq(value.Origin{})},
		)},
	)
	if got, want := display(unsorted), `{"a": {"a": [], "z": null}, "z": "last"}`; got != want {
		t.Fatalf("object diagnostic order=%s, want %s", got, want)
	}
}

func TestUnknownMatrixAxisDiagnosticUsesKeyOrigin(t *testing.T) {
	source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        os: [ubuntu]\n        arch: [arm]\n        exclude:\n          - missing:\n              nested: [null, {z: value}]\n    steps: [{run: echo ok}]\n"
	workflow, errors := workflowlang.Parse([]byte(source))
	if len(errors) != 0 {
		t.Fatal(errors)
	}
	resolution := Resolve(workflow.Jobs["test"].Strategy)
	findings := resolution.Check(false)
	wantPosition := diagnostic.Position{Line: 10, Column: 13}
	wantMessage := `"missing" in "exclude" section does not exist in matrix. available matrix configurations are "arch", "os"`
	if len(findings) != 1 || findings[0].Start != wantPosition || findings[0].Message != wantMessage {
		t.Fatalf("unknown axis diagnostic=%v, want %v: %s", findings, wantPosition, wantMessage)
	}
	original := resolution.exclude[0]
	newOrigin := value.Origin{Span: value.Span{Start: diagnostic.Position{Line: 25, Column: 30}}}
	derived := original.With("extra", value.YAMLScalar("new", "!!str", newOrigin))
	replaced := derived.With("MISSING", value.YAMLScalar("replacement", "!!str", newOrigin))
	if original.keyOrigin("missing").Span.Start != wantPosition || derived.keyOrigin("missing").Span.Start != wantPosition || replaced.keyOrigin("missing").Span.Start != wantPosition || derived.keyOrigin("extra") != newOrigin {
		t.Fatal("Combination.With lost declaration origins")
	}
	if _, changed := original.Get("extra"); changed {
		t.Fatal("Combination.With mutated original values")
	}
	if _, changed := original.keyOrigins["extra"]; changed {
		t.Fatal("Combination.With mutated original key origins")
	}
}

func TestExpressionProducedMatrixKeyUsesAvailableOrigin(t *testing.T) {
	for _, declaration := range []string{
		`exclude: "${{ fromJSON('[{\"missing\":{\"nested\":null}}]') }}"`,
		`exclude: ["${{ fromJSON('{\"missing\":{\"nested\":null}}') }}"]`,
	} {
		workflow, errors := workflowlang.Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        os: [ubuntu]\n        " + declaration + "\n    steps: [{run: echo ok}]\n"))
		if len(errors) != 0 {
			t.Fatal(errors)
		}
		matrix := workflow.Jobs["test"].Strategy.Matrix
		source := matrix.Exclude.Expression
		if source == nil {
			source = matrix.Exclude.Combinations[0].Expression
		}
		if source == nil || source.Pos == nil {
			t.Fatal("missing filter expression source")
		}
		findings := Resolve(workflow.Jobs["test"].Strategy).Check(false)
		if len(findings) != 1 || findings[0].Start.Line != source.Pos.Line || findings[0].Start.Column != source.Pos.Col {
			t.Fatalf("expression object fabricated key position: %v", findings)
		}
	}
}

func TestNestedMatrixMismatchDiagnosticKeepsValues(t *testing.T) {
	workflow, errors := workflowlang.Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        item: [{z: [null, {b: old, a: true}], a: 1}]\n        exclude: [{item: {z: [null, {b: new, a: true}]}}]\n    steps: [{run: echo ok}]\n"))
	if len(errors) != 0 {
		t.Fatal(errors)
	}
	findings := Resolve(workflow.Jobs["test"].Strategy).Check(false)
	want := `value {"z": [null, {"a": true, "b": "new"}]} in "exclude" does not match in matrix "item" combinations. possible values are {"a": 1, "z": [null, {"a": true, "b": "old"}]}`
	if len(findings) != 1 || findings[0].Message != want {
		t.Fatalf("nested mismatch diagnostic=%v, want %s", findings, want)
	}
}
