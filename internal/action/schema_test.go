package action

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml"
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

func TestSchemaActionEveryProperty(t *testing.T) {
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
			// These values have runtime-specific diagnostics in the action-rule adapter.
			// TestActionStepValueShapes exercises that integration boundary.
			if definition == "run-step" && (property == "run" || property == "shell") || definition == "uses-step" && property == "uses" {
				continue
			}
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
				var meta Metadata
				if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")+"\n"), &meta); err != nil {
					return
				}
				if len(Validate(&meta)) == 0 {
					t.Fatal("invalid property shape was accepted")
				}
			})
		}
	}
}

// Cases follow actions/runner action_yaml.json at 759385a3510197a58b5c08dc1f373b74b9f4643b.
