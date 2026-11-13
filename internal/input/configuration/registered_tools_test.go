package config

import (
	"encoding/json"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests/configschema"
)

func TestRegisteredToolSettingsForFiles(t *testing.T) {
	t.Parallel()
	metadata := configschema.Settings{
		Schema:     json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1}},"additionalProperties":false}`),
		Shorthands: map[string]configschema.Shorthand{"limit": {Path: []string{"limit"}, Precedence: configschema.PrecedenceShorthand}},
	}
	registration := RuleRegistration{Descriptor: rule.Descriptor{Name: "file-tool", Category: "correctness", Maturity: "stable"}, Scope: rule.SourceConfiguration, ToolSettings: &metadata}
	rules, err := (RuleSet{}).WithRegisteredRules(registration)
	if err != nil {
		t.Fatal(err)
	}
	metadata.Shorthands["limit"].Path[0] = "broken"
	clear(metadata.Schema)
	cfg, err := rules.Parse([]byte("tools:\n  file-tool: {limit: 3}\noverrides:\n  - includes: ['inner/**']\n    tools:\n      file-tool: {limit: 7}\n"))
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
	if _, err := (RuleSet{}).Parse([]byte("tools: {file-tool: {limit: 3}}")); err == nil {
		t.Fatal("provider metadata leaked into another rule set")
	}
}
