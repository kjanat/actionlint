package validation_test

import (
	"fmt"
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/internal/action"
	actioncheck "actionlint.kjanat.dev/internal/action/validation"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/yaml"
)

func TestActionMetadataKnownExpressions(t *testing.T) {
	const base = `name: audit
description: audit
runs:
  using: composite
  steps:
    - uses: actions/checkout@v6
`
	for _, tc := range []struct {
		name, field, value, message, path string
	}{
		{"env string", "env", `${{ 'not-a-map' }}`, "expected a mapping", "env"},
		{"env empty string", "env", `${{ '' }}`, "expected a mapping", "env"},
		{"env integer", "env", `${{ 1 }}`, "expected a mapping", "env"},
		{"env float", "env", `${{ 1.5 }}`, "expected a mapping", "env"},
		{"env boolean", "env", `${{ true }}`, "expected a mapping", "env"},
		{"env null", "env", `${{ null }}`, "expected a mapping", "env"},
		{"with string", "with", `${{ 'not-a-map' }}`, "expected a mapping", "with"},
		{"with array", "with", `${{ fromJSON('[]') }}`, "expected a mapping", "with"},
		{"env JSON scalar", "env", `${{ fromJSON('false') }}`, "expected a mapping", "env"},
		{"env nested array", "env", `${{ fromJSON('{"VALUE":[]}') }}`, "expected a scalar string", "env.VALUE"},
		{"with nested mapping", "with", `${{ fromJSON('{"value":{"nested":"value"}}') }}`, "expected a scalar string", "with.value"},
		{"env empty key", "env", `${{ fromJSON('{"":"value"}') }}`, "expected a non-empty scalar key", "env"},
		{"continue boolean", "continue-on-error", `${{ true }}`, "", ""},
		{"continue JSON boolean", "continue-on-error", `${{ fromJSON('false') }}`, "", ""},
		{"continue string", "continue-on-error", `${{ 'true' }}`, "expected a boolean or expression", "continue-on-error"},
		{"continue integer", "continue-on-error", `${{ 1 }}`, "expected a boolean or expression", "continue-on-error"},
		{"continue array", "continue-on-error", `${{ fromJSON('[]') }}`, "expected a boolean or expression", "continue-on-error"},
		{"string from array", "name", `${{ fromJSON('[]') }}`, "expected a scalar string", "name"},
		{"string from object", "name", `${{ fromJSON('{}') }}`, "expected a scalar string", "name"},
		{"run from array", "run", `${{ fromJSON('[]') }}`, "expected a scalar string", "run"},
		{"shell from object", "shell", `${{ fromJSON('{}') }}`, "expected a scalar string", "shell"},
		{"run from boolean", "run", `${{ false }}`, "", ""},
		{"shell string expression", "shell", `${{ 'bash' }}`, "", ""},
		{"if array truthiness", "if", `${{ fromJSON('[]') }}`, "", ""},
		{"if object truthiness", "if", `${{ fromJSON('{}') }}`, "", ""},
		{"string from boolean", "name", `${{ false }}`, "", ""},
		{"string from null", "name", `${{ null }}`, "", ""},
		{"empty string allowed", "name", `${{ '' }}`, "", ""},
		{"env empty map", "env", `${{ fromJSON('{}') }}`, "", ""},
		{"env scalar values", "env", `${{ fromJSON('{"TEXT":"value","NUMBER":1,"BOOL":false,"NULL":null}') }}`, "", ""},
		{"with scalar values", "with", `${{ fromJSON('{"text":"value","number":1.5,"boolean":true,"null":null}') }}`, "", ""},
		{"env unknown input", "env", `${{ inputs.value }}`, "", ""},
		{"with unknown JSON", "with", `${{ fromJSON(inputs.value) }}`, "", ""},
		{"continue unknown value", "continue-on-error", `${{ inputs.optional == 'true' }}`, "", ""},
		{"string interpolation", "name", `prefix-${{ inputs.value }}`, "", ""},
		{"env interpolation", "env", `prefix-${{ inputs.value }}`, "expected a mapping", "env"},
		{"quoted delimiters are string data", "name", `${{ '}} ${{ secrets.TOKEN }}' }}`, "", ""},
		{"quoted expression is not a mapping", "env", `${{ '${{ inputs.value }}' }}`, "expected a mapping", "env"},
		{"evaluated values are data", "env", `${{ fromJSON('{"VALUE":"${{ secrets.TOKEN }}"}') }}`, "", ""},
		{"evaluated keys are data", "env", `${{ fromJSON('{"${{ secrets.TOKEN }}":"value"}') }}`, "", ""},
		{"evaluated insert key is data", "env", `${{ fromJSON('{"${{ insert }}":"value"}') }}`, "", ""},
		{"evaluated quoted key is not folded", "with", `${{ fromJSON('{"${{ '''' }}":"value"}') }}`, "", ""},
		{"evaluated quoted value is not folded", "env", `${{ fromJSON('{"VALUE":"${{ fromJSON(''[]'') }}"}') }}`, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefix := base
			switch tc.field {
			case "run":
				prefix = strings.Replace(base, "uses: actions/checkout@v6", "shell: bash", 1)
			case "shell":
				prefix = strings.Replace(base, "uses: actions/checkout@v6", "run: echo ok", 1)
			}
			source := []byte(prefix + "      " + tc.field + ": " + tc.value + "\n")
			var meta actionmeta.Metadata
			if err := yaml.Unmarshal(source, &meta); err != nil {
				t.Fatal(err)
			}
			meta.Filename, meta.Source = "action.yml", source
			findings := actioncheck.Check(&meta, &workflowlang.Pos{Line: 1, Col: 1})
			errs := findings
			if tc.message == "" {
				if len(errs) != 0 {
					t.Fatalf("valid action rejected: %v", errs)
				}
				return
			}
			want := fmt.Sprintf("%s at %q in action metadata", tc.message, "runs.steps[0]."+tc.path)
			if len(errs) != 1 || errs[0].Message != want {
				t.Fatalf("wanted one diagnostic %q, got %v", want, errs)
			}
			if got := errs[0]; got.Start.Line != 7 || got.Start.Column != len(tc.field)+9 || got.Path != "action.yml" {
				t.Fatalf("diagnostic lost expression source position: %v", got)
			}
		})
	}
}

func TestActionJSONMemberCollisions(t *testing.T) {
	violations := actionmeta.ExpressionViolations(`${{ fromJSON('{"VALUE":"a","value":"b"}') }}`, false, "runs.steps.*.env", nil)
	if len(violations) != 1 || !strings.Contains(violations[0].Message, "collide under case-insensitive lookup") {
		t.Fatal(violations)
	}
}
