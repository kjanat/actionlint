package configuration

import (
	"encoding/json"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/model/rules"
	"actionlint.kjanat.dev/v2/internal/model/toolsettings"
)

func TestRegisteredToolSettingsForFiles(t *testing.T) {
	t.Parallel()
	metadata := toolsettings.Settings{
		Schema:     json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1}},"additionalProperties":false}`),
		Shorthands: map[string]toolsettings.Shorthand{"limit": {Path: []string{"limit"}, Precedence: toolsettings.PrecedenceShorthand}},
	}
	registration := RuleRegistration{Declaration: rule.Declaration{Descriptor: rule.Descriptor{Name: "file-tool", Category: "correctness", Maturity: "stable"}, Scope: rule.SourceConfiguration}, ToolSettings: &metadata}
	rules, err := (RuleSet{}).WithRegisteredRules(registration)
	if err != nil {
		t.Fatal(err)
	}
	metadata.Shorthands["limit"].Path[0] = "broken"
	clear(metadata.Schema)
	cfg, err := rules.ParseConfig([]byte("tools:\n  file-tool: {limit: 3}\noverrides:\n  - includes: ['inner/**']\n    tools:\n      file-tool: {limit: 7}\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		want int
	}{{"outer.yml", 3}, {"inner/workflow.yml", 7}} {
		selected, err := rules.ForFile(cfg, tc.path, "")
		if err != nil {
			t.Fatal(err)
		}
		got := selected.Tools["file-tool"].Config.Inline()["limit"]
		if got != tc.want {
			t.Fatalf("%s: limit = %#v, want %d", tc.path, got, tc.want)
		}
	}
	if _, err := (RuleSet{}).ParseConfig([]byte("tools: {file-tool: {limit: 3}}")); err == nil {
		t.Fatal("provider metadata leaked into another rule set")
	}
}

func TestToolOverridePreservesUntouchedNumericType(t *testing.T) {
	t.Parallel()
	metadata := toolsettings.Settings{Schema: json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer"}},"additionalProperties":false}`)}
	rules, err := (RuleSet{}).WithRegisteredRules(
		RuleRegistration{Declaration: rule.Declaration{Descriptor: rule.Descriptor{Name: "file-tool", Category: "correctness", Maturity: "stable"}, Scope: rule.SourceConfiguration}, ToolSettings: &metadata},
		RuleRegistration{Declaration: rule.Declaration{Descriptor: rule.Descriptor{Name: "other-tool", Category: "correctness", Maturity: "stable"}, Scope: rule.SourceConfiguration}, ToolSettings: &metadata})
	if err != nil {
		t.Fatal(err)
	}
	for _, overridden := range []string{"file-tool", "other-tool"} {
		t.Run(overridden, func(t *testing.T) {
			t.Parallel()
			text := "tools: {file-tool: {config: {limit: 3}}, other-tool: {config: {limit: 9}}}\noverrides: [{includes: ['**'], tools: {" + overridden + ": {enabled: false}}}]\n"
			cfg, err := rules.ParseConfig([]byte(text))
			if err != nil {
				t.Fatal(err)
			}
			projected, err := rules.ForFile(cfg, "ci.yml", "")
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range map[string]int{"file-tool": 3, "other-tool": 9} {
				got := projected.Tools[name].Config.Inline()["limit"]
				if value, ok := got.(int); !ok || value != want {
					t.Fatalf("%s untouched limit changed type or value: %T(%v)", name, got, got)
				}
			}
		})
	}
}
