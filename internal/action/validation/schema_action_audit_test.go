package validation_test

import (
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/internal/action"
	actioncheck "actionlint.kjanat.dev/internal/action/validation"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/yaml"
)

func TestSchemaActionDynamicRequiredInputs(t *testing.T) {
	meta := &actionmeta.Metadata{Inputs: actionmeta.Inputs{"token": {Name: "token", Required: true}}}
	for _, dynamic := range []bool{false, true} {
		exec := &workflowlang.ExecAction{Uses: &workflowlang.String{Value: "./audit", Pos: &workflowlang.Pos{Line: 1, Col: 1}}}
		if dynamic {
			exec.InputsExpression = &workflowlang.String{Value: "${{ fromJSON(inputs.parameters) }}", Pos: &workflowlang.Pos{Line: 2, Col: 1}}
		}
		findings := actioncheck.Inputs(meta, exec, func(*actionmeta.Metadata) string { return "audit" })
		if (len(findings) == 0) != dynamic {
			t.Fatalf("dynamic=%v; diagnostics=%v", dynamic, findings)
		}
	}
}

func TestSchemaActionAudit(t *testing.T) {
	const composite = `name: audit
description: audit
runs:
  using: composite
  steps:
    - run: echo ok
      shell: bash
`
	const docker = `name: audit
description: audit
runs:
  using: docker
  image: docker://alpine:3
`
	tests := []struct {
		name, source string
		valid        bool
	}{
		{"composite", composite, true},
		{"metadata explicit non-core boolean", strings.Replace(composite, "name: audit", "name: !!bool on", 1), false},
		{"metadata explicit non-core integer", strings.Replace(composite, "name: audit", "name: !!int 0b10", 1), false},
		{"metadata quoted numeric tag", strings.Replace(composite, "name: audit", "name: !!float '1.5'", 1), false},
		{"metadata custom scalar tag", strings.Replace(composite, "name: audit", "name: !custom value", 1), false},
		{"metadata custom extension scalar tag", composite + "x-extension: !custom value\n", false},
		{"docker hooks", docker + `  pre-if: always()
  post-if: always()
`, true},
		{"docker boolean hook conditions", docker + `  pre-if: true
  post-if: false
`, true},
		{"docker image entrypoints", docker + `  pre-entrypoint: /usr/local/bin/setup
  entrypoint: /usr/local/bin/run
  post-entrypoint: /usr/local/bin/cleanup
`, true},
		{"output context", composite + `outputs:
  result:
    value: ${{ secrets.TOKEN }}
`, false},
		{"output mapping", composite + `outputs:
  result: []
`, false},
		{"output unknown key", composite + `outputs:
  result:
    typo: nope
`, false},
		{"output value shape", composite + `outputs:
  result:
    value: []
`, false},
		{"output key empty", composite + `outputs:
  '': {}
`, false},
		{"output value valid", composite + `outputs:
  result:
    description: example
    value: ${{ inputs.value }}
`, true},
		{"docker args context", docker + "  args: ['${{ github.token }}']\n", false},
		{"docker args shape", docker + "  args: [[invalid]]\n", false},
		{"docker env context", docker + `  env:
    TOKEN: ${{ secrets.TOKEN }}
`, false},
		{"docker env value shape", docker + `  env:
    VALUE: []
`, false},
		{"docker env key empty", docker + `  env:
    '': nope
`, false},
		{"runs unknown key", docker + "  typo: true\n", false},
		{"runs empty optional string", docker + "  entrypoint: ''\n", false},
		{"runs forbidden empty field", docker + "  steps: []\n", false},
		{"composite name shape", composite + "      name: []\n", false},
		{"composite id shape", composite + "      id: {}\n", false},
		{"composite empty id", composite + "      id: ''\n", false},
		{"composite id characters", composite + "      id: bad.id\n", false},
		{"composite id reserved", composite + "      id: __internal\n", false},
		{"composite id maximum", composite + "      id: " + strings.Repeat("a", 100) + "\n", false},
		{"composite id length boundary", composite + "      id: " + strings.Repeat("a", 99) + "\n", true},
		{"composite duplicate id", composite + `      id: build
    - run: echo next
      shell: bash
      id: BUILD
`, false},
		{"composite literal id", composite + "      id: ${{ 'build' }}\n", true},
		{"composite if shape", composite + "      if: []\n", false},
		{"composite env shape", composite + "      env: []\n", false},
		{"composite env nested shape", composite + "      env: {TOKEN: []}\n", false},
		{"composite env expression", composite + "      env: ${{ fromJSON(inputs.env) }}\n", true},
		{"composite env expression key", composite + `      env:
        '${{ inputs.key }}': value
`, true},
		{"composite env unavailable key context", composite + `      env:
        '${{ secrets.TOKEN }}': value
`, false},
		{"composite env insert", composite + `      env:
        '${{ insert }}': {VALUE: text}
`, true},
		{"composite env compact insert", composite + `      env:
        '${{insert}}': {VALUE: text}
`, true},
		{"composite env insert shape", composite + `      env:
        '${{ insert }}': []
`, false},
		{"composite recursive insertion", composite + `      env: &recursive
        '${{ insert }}': *recursive
`, false},
		{"composite continue literal", composite + "      continue-on-error: nonsense\n", false},
		{"composite continue boolean", composite + "      continue-on-error: true\n", true},
		{"composite explicit non-core boolean", composite + "      continue-on-error: !!bool on\n", false},
		{"composite continue expression", composite + "      continue-on-error: ${{ inputs.optional == 'true' }}\n", true},
		{"composite working directory shape", composite + "      working-directory: []\n", false},
		{"composite numeric script", strings.Replace(composite, "run: echo ok", "run: 42", 1), true},
		{"composite escaped expression marker", strings.Replace(composite, "run: echo ok", "run: ${{ '${{ secrets.TOKEN }}' }}", 1), true},
		{"composite malformed expression", strings.Replace(composite, "run: echo ok", "run: echo ${{ }}", 1), false},
		{"composite unknown function", strings.Replace(composite, "run: echo ok", "run: echo ${{ nonexistent() }}", 1), false},
		{"composite case missing default", strings.Replace(composite, "run: echo ok", "run: echo ${{ case(true, 'yes') }}", 1), false},
		{"composite case valid", strings.Replace(composite, "run: echo ok", "run: echo ${{ case(true, 'yes', 'no') }}", 1), true},
		{"composite folded condition trailing text", composite + "      if: ${{ 'github.ref }} ignored' }}\n", false},
		{"composite bare condition trailing text", composite + "      if: github.ref }} ignored\n", false},
		{"composite folded condition quoted delimiter", composite + "      if: ${{ 'contains(github.ref, ''}}'')' }}\n", true},
		{"composite format single argument", strings.Replace(composite, "run: echo ok", "run: echo ${{ format('yes') }}", 1), true},
		{"composite unterminated expression", strings.Replace(composite, "run: echo ok", "run: echo ${{ inputs.value", 1), false},
		{"composite uses expression", strings.Replace(composite, "run: echo ok\n      shell: bash", "uses: ${{ inputs.action }}", 1), false},
		{"input null mapping", composite + `inputs:
  value:
`, false},
		{"input empty key", composite + `inputs:
  '': {}
`, false},
		{"literal runtime", strings.Replace(docker, "using: docker", "using: ${{ 'docker' }}", 1), true},
		{"internal plugin runtime", `name: audit
description: audit
runs:
  plugin: Runner.Plugins.Example
`, true},
		{"plugin closed properties", `name: audit
description: audit
runs:
  plugin: Runner.Plugins.Example
  main: main.js
`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var meta actionmeta.Metadata
			err := yaml.Unmarshal([]byte(tc.source), &meta)
			var findings []*findingmodel.Finding
			if err == nil {
				meta.Filename = "action.yml"
				meta.Source = []byte(tc.source)
				findings = actioncheck.Check(&meta, &workflowlang.Pos{Line: 1, Col: 1})
			}
			valid := err == nil && len(findings) == 0
			if valid != tc.valid {
				t.Errorf("valid=%v, want %v; parse=%v; diagnostics=%v", valid, tc.valid, err, findings)
			}
		})
	}
}

func TestActionStepValueShapes(t *testing.T) {
	for _, declaration := range []string{"run: [invalid]\n      shell: bash", "run: echo ok\n      shell: [invalid]", "uses: [invalid]"} {
		t.Run(declaration, func(t *testing.T) {
			source := []byte("name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - " + declaration + "\n")
			var meta actionmeta.Metadata
			if err := yaml.Unmarshal(source, &meta); err != nil {
				t.Fatal(err)
			}
			findings := actioncheck.Check(&meta, &workflowlang.Pos{Line: 1, Col: 1})
			if len(findings) == 0 {
				t.Fatal("invalid step value was accepted")
			}
		})
	}
}
