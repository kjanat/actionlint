package actionlint

import (
	"strings"
	"testing"
)

func TestKnownExpressionActionInputs(t *testing.T) {
	meta := &ActionMetadata{Inputs: ActionMetadataInputs{
		"token": {Name: "token", Required: true},
		"old":   {Name: "old", Deprecated: true},
	}}
	for _, tc := range []struct {
		name, expression, want string
	}{
		{"missing", `fromJSON('{}')`, `missing input "token"`},
		{"provided", `fromJSON('{"token":"value"}')`, ""},
		{"case insensitive", `fromJSON('{"TOKEN":"value"}')`, ""},
		{"scalar coercion", `fromJSON('{"token":false}')`, ""},
		{"unknown input", `fromJSON('{"token":"value","typo":"x"}')`, `input "typo" is not defined`},
		{"deprecated input", `fromJSON('{"token":"value","old":"x"}')`, `avoid using deprecated input "old"`},
		{"dynamic", `fromJSON(vars.INPUTS)`, ""},
		{"invalid shape handled by expression rule", `fromJSON('[]')`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, errs := Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/action@v1\n        with: ${{ " + tc.expression + " }}\n"))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			exec := workflow.Jobs["test"].Steps[0].Exec.(*ExecAction)
			rule := NewRuleAction(nil)
			rule.checkAction(meta, exec, func(*ActionMetadata) string { return "example/action@v1" })
			errs = rule.Errs()
			if tc.want == "" && len(errs) != 0 || tc.want != "" && (len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want)) {
				t.Fatalf("wanted %q, got %v", tc.want, errs)
			}
		})
	}
}

func TestKnownExpressionRunnerLabels(t *testing.T) {
	for _, tc := range []struct {
		name, runsOn, want string
	}{
		{"object typo", `${{ fromJSON('{"labels":["ubuntu-lates"]}') }}`, `label "ubuntu-lates" is unknown`},
		{"array typo", `${{ fromJSON('["ubuntu-lates"]') }}`, `label "ubuntu-lates" is unknown`},
		{"string typo", `${{ 'ubuntu-lates' }}`, `label "ubuntu-lates" is unknown`},
		{"valid object", `${{ fromJSON('{"labels":["ubuntu-latest"]}') }}`, ""},
		{"casing", `${{ fromJSON('{"LABELS":["UBUNTU-LATEST"]}') }}`, ""},
		{"group is not label", `${{ fromJSON('{"group":"custom-group"}') }}`, ""},
		{"group and labels", `${{ fromJSON('{"group":"custom-group","labels":"linux"}') }}`, ""},
		{"group does not hide typo", `${{ fromJSON('{"group":"custom-group","labels":"ubuntu-lates"}') }}`, `label "ubuntu-lates" is unknown`},
		{"conflicting array", `${{ fromJSON('["ubuntu-latest","windows-latest"]') }}`, `label "windows-latest" conflicts`},
		{"dynamic", `${{ fromJSON(vars.RUNNERS) }}`, ""},
		{"interpolation", `prefix-${{ 'ubuntu-lates' }}`, ""},
		{"invalid object label handled by expression rule", `${{ fromJSON('{"labels":[{}]}') }}`, ""},
		{"null handled by expression rule", `${{ fromJSON('{"labels":[null]}') }}`, ""},
		{"number coercion", `${{ fromJSON('{"labels":[42]}') }}`, `label "42" is unknown`},
		{"boolean coercion", `${{ fromJSON('{"labels":[false]}') }}`, `label "false" is unknown`},
		{"labels expression", `{labels: "${{ fromJSON('[\"ubuntu-lates\"]') }}"}`, `label "ubuntu-lates" is unknown`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, errs := Parse([]byte("on: push\njobs:\n  test:\n    runs-on: " + tc.runsOn + "\n    steps:\n      - run: echo ok\n"))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := NewRuleRunnerLabel()
			visitor := NewVisitor()
			visitor.AddPass(rule)
			if err := visitor.Visit(workflow); err != nil {
				t.Fatal(err)
			}
			errs = rule.Errs()
			if tc.want == "" && len(errs) != 0 || tc.want != "" && (len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want)) {
				t.Fatalf("wanted %q, got %v", tc.want, errs)
			}
		})
	}
}
