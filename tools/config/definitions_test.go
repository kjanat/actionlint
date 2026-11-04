package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	actionlint "actionlint.kjanat.dev/v2/internal/config"
	lintconfig "actionlint.kjanat.dev/v2/internal/config/rules"
	"actionlint.kjanat.dev/v2/internal/editorschema"
	"actionlint.kjanat.dev/v2/rule"
)

func TestSchemaSharesDefinitionsAndRuleDescriptions(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(generatedSchema(t), &root); err != nil {
		t.Fatal(err)
	}
	definitions, ok := root["$defs"].(map[string]any)
	if !ok {
		t.Fatal("shared definitions missing")
	}
	var declarations []rule.Descriptor
	for _, entry := range actionlint.Rules() {
		declarations = append(declarations, entry.Descriptor)
	}
	expected, err := json.Marshal(editorschema.RuleNameChoices(declarations))
	if err != nil {
		t.Fatal(err)
	}
	var choices any
	if err := json.Unmarshal(expected, &choices); err != nil {
		t.Fatal(err)
	}
	for _, value := range choices.([]any) {
		entry := value.(map[string]any)
		if description, ok := entry["description"].(string); ok && description != "" {
			entry["markdownDescription"] = description
		}
	}
	knownNames, ok := definitions["known-rule-name"].(map[string]any)
	if !ok || !reflect.DeepEqual(knownNames["anyOf"], choices) {
		t.Fatal("shared rule names differ from the current catalogue")
	}
	for _, name := range []string{"level", "group-level", "preset", "rule-setting", "plugin-setting", "backend", "rule-name", "known-rule-name"} {
		if _, exists := definitions[name]; !exists {
			t.Errorf("missing shared definition %s", name)
		}
	}
	levelEnums, presetEnums, describedRules := 0, 0, 0
	refs := map[string]int{}
	var walk func(any)
	walk = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if reflect.DeepEqual(node["enum"], enumValues(lintconfig.Levels())) {
				levelEnums++
			}
			if reflect.DeepEqual(node["enum"], enumValues(lintconfig.Presets())) {
				presetEnums++
			}
			if ref, ok := node["$ref"].(string); ok {
				refs[ref]++
				if ref == "#/$defs/rule-setting" || ref == "#/$defs/plugin-setting" {
					if description, ok := node["description"].(string); ok && description != "" {
						describedRules++
					}
				}
			}
			for _, child := range node {
				walk(child)
			}
		case []any:
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(root)
	if levelEnums != 1 || presetEnums != 1 {
		t.Fatalf("enum definitions copied: levels=%d presets=%d", levelEnums, presetEnums)
	}
	if describedRules < 2*len(ruleNames()) {
		t.Fatalf("rule-specific descriptions lost: %d", describedRules)
	}
	for _, name := range []string{"level", "group-level", "preset", "backend"} {
		if refs["#/$defs/"+name] < 2 {
			t.Errorf("shared definition %s is not reused", name)
		}
	}
	data, _ := json.Marshal(definitions["rule-setting"])
	if !strings.Contains(string(data), `"$ref":"#/$defs/plugin-setting"`) {
		t.Fatal("plain settings duplicate the plugin setting contract")
	}
}
