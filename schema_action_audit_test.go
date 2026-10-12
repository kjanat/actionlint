package actionlint

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestSchemaActionDefinitionCoverage(t *testing.T) {
	want := strings.Fields("action-root boolean-steps-context composite-runs composite-step composite-steps container-runs container-runs-args container-runs-context container-runs-env input input-default-context inputs node-runs non-empty-string output-definition output-value outputs plugin-runs run-step runs step-env step-if step-with string-steps-context uses-step")
	if got := slices.Sorted(maps.Keys(actionMetadataSchema)); !slices.Equal(got, want) {
		t.Fatalf("runner definition inventory changed: %v", got)
	}
	if !slices.Equal(actionMetadataSchema["runs"].variants, []string{"container-runs", "node-runs", "plugin-runs", "composite-runs"}) {
		t.Fatal("runs alternatives changed; update runtime selection")
	}
	if !slices.Equal(actionMetadataSchema["composite-step"].variants, []string{"run-step", "uses-step"}) {
		t.Fatal("composite step alternatives changed; update step selection")
	}
}

func TestSchemaActionDynamicRequiredInputs(t *testing.T) {
	meta := &ActionMetadata{Inputs: ActionMetadataInputs{"token": {Name: "token", Required: true}}}
	for _, dynamic := range []bool{false, true} {
		exec := &ExecAction{Uses: &String{Value: "./audit", Pos: &Pos{Line: 1, Col: 1}}}
		if dynamic {
			exec.InputsExpression = &String{Value: "${{ fromJSON(inputs.parameters) }}", Pos: &Pos{Line: 2, Col: 1}}
		}
		rule := NewRuleAction(nil)
		rule.checkAction(meta, exec, func(*ActionMetadata) string { return "audit" })
		if (len(rule.Errs()) == 0) != dynamic {
			t.Fatalf("dynamic=%v; diagnostics=%v", dynamic, rule.Errs())
		}
	}
}

func TestSchemaActionEveryProperty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.js"), []byte("// test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const header = `name: audit
description: audit
`
	const composite = `runs:
  using: composite
  steps:
    - run: echo ok
      shell: bash
`
	fixtures := map[string]struct{ source, indent string }{
		"action-root": {header + composite, ""},
		"input": {header + composite + `inputs:
  value:
    description: input
`, "    "},
		"output-definition": {header + composite + `outputs:
  value:
    description: output
`, "    "},
		"container-runs": {header + `runs:
  using: docker
  image: docker://alpine:3
`, "  "},
		"node-runs": {header + `runs:
  using: node24
  main: main.js
`, "  "},
		"plugin-runs": {header + `runs:
  plugin: Runner.Plugins.Example
`, "  "},
		"composite-runs": {header + composite, "  "},
		"run-step":       {header + composite, "      "},
		"uses-step": {header + `runs:
  using: composite
  steps:
    - uses: actions/checkout@v6
`, "      "},
	}
	for definition, fixture := range fixtures {
		for property, child := range actionMetadataSchema[definition].properties {
			t.Run(definition+"/"+property, func(t *testing.T) {
				// Invalid shapes must be found even on optional fields. Replace an
				// existing field, preserving the surrounding valid action.
				invalid := "[]"
				if actionMetadataSchema[child].kind == actionSchemaSequence {
					invalid = "{}"
				}
				prefix := fixture.indent + property + ":"
				lines := strings.Split(strings.TrimSuffix(fixture.source, "\n"), "\n")
				found := false
				for i, line := range lines {
					if strings.HasPrefix(line, prefix) || strings.HasPrefix(line, fixture.indent[:max(0, len(fixture.indent)-2)]+"- "+property+":") {
						colon := strings.IndexByte(line, ':')
						lines[i] = line[:colon+1] + " " + invalid
						// Drop that field's children when replacing a collection.
						end := i + 1
						for end < len(lines) && len(lines[end])-len(strings.TrimLeft(lines[end], " ")) > len(fixture.indent) {
							end++
						}
						lines = append(lines[:i+1], lines[end:]...)
						found = true
						break
					}
				}
				if !found {
					lines = append(lines, prefix+" "+invalid)
				}
				var meta ActionMetadata
				if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")+"\n"), &meta); err != nil {
					return
				}
				meta.dir, meta.file = dir, "action.yml"
				rule := NewRuleAction(nil)
				rule.checkLocalActionMetadata(&meta, &ExecAction{Uses: &String{Value: "./audit", Pos: &Pos{Line: 1, Col: 1}}})
				if len(rule.Errs()) == 0 {
					t.Fatal("invalid property shape was accepted")
				}
			})
		}
	}
}

// Cases follow actions/runner action_yaml.json at 759385a3510197a58b5c08dc1f373b74b9f4643b.
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
			var meta ActionMetadata
			err := yaml.Unmarshal([]byte(tc.source), &meta)
			rule := NewRuleAction(nil)
			if err == nil {
				meta.file = "action.yml"
				meta.src = []byte(tc.source)
				rule.checkLocalActionMetadata(&meta, &ExecAction{Uses: &String{Value: "./audit", Pos: &Pos{Line: 1, Col: 1}}})
			}
			valid := err == nil && len(rule.Errs()) == 0
			if valid != tc.valid {
				t.Errorf("valid=%v, want %v; parse=%v; diagnostics=%v", valid, tc.valid, err, rule.Errs())
			}
		})
	}
}
