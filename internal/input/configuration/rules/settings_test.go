package rules

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kjanat/go-yaml/v4"
)

func TestSettingCanonicalLevels(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"false", "off"},
		{"true", "on"},
		{"{level: true}", "on"},
		{"{level: false}", "off"},
		{"null", "default"},
		{"{level: null}", "default"},
		{"off", "off"},
		{"default", "default"},
		{"{level: default}", "default"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			var document yaml.Node
			if err := yaml.Unmarshal([]byte(tc.input), &document); err != nil {
				t.Fatal(err)
			}
			setting, err := parseSetting(document.Content[0])
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := yaml.Marshal(setting)
			if err != nil {
				t.Fatal(err)
			}
			var normalized struct {
				Level string `yaml:"level"`
			}
			if err := yaml.Unmarshal(encoded, &normalized); err != nil {
				t.Fatal(err)
			}
			if normalized.Level != tc.want {
				t.Fatalf("got %s, want %s", normalized.Level, tc.want)
			}
		})
	}
}

// editingOptions overwrites the node it decodes, as a careless UnmarshalYAML might.
type editingOptions struct{ name string }

func (o *editingOptions) UnmarshalYAML(n *yaml.Node) error {
	o.name = n.Content[1].Value
	n.Content[1].Value = "edited"
	return nil
}

func TestLoadOptionsDecodesPrivateCopy(t *testing.T) {
	setting, err := NewSetting("on", map[string]string{"name": "original"})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		var options editingOptions
		if loaded, err := setting.LoadOptions(&options); !loaded || err != nil || options.name != "original" {
			t.Fatalf("loaded=%v err=%v name=%q", loaded, err, options.name)
		}
	}
}

func TestNewSettingRejectsNonMappingOptions(t *testing.T) {
	if _, err := NewSetting("on", []string{"a"}); err == nil {
		t.Fatal("accepted sequence options")
	}
	setting, err := NewSetting("warn", nil)
	if err != nil || setting.HasOptions() || setting.Level() != "warn" {
		t.Fatalf("setting=%+v err=%v", setting, err)
	}
}

func TestNewSettingParsesJSONRawMessage(t *testing.T) {
	setting, err := NewSetting("on", json.RawMessage(`{"max-count": 3, "names": ["a"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var options struct {
		MaxCount int      `yaml:"max-count"`
		Names    []string `yaml:"names"`
	}
	if loaded, err := setting.LoadOptions(&options); !loaded || err != nil || options.MaxCount != 3 || len(options.Names) != 1 || options.Names[0] != "a" {
		t.Fatalf("loaded=%v err=%v options=%+v", loaded, err, options)
	}
	for _, raw := range []json.RawMessage{nil, json.RawMessage("null")} {
		if setting, err := NewSetting("on", raw); err != nil || setting.HasOptions() {
			t.Fatalf("%q: setting=%+v err=%v", raw, setting, err)
		}
	}
	for _, raw := range []json.RawMessage{json.RawMessage("[1]"), json.RawMessage("{"), json.RawMessage("max-count: 3")} {
		if _, err := NewSetting("on", raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestGroupKeepsItsRulesAndOmitsWhenEmpty(t *testing.T) {
	rules := map[string]Setting{"expression": {level: "off"}}
	group := NewGroup("", "warn", rules)
	rules["expression"] = Setting{level: "on"}
	if setting, ok := group.Rule("expression"); !ok || setting.Level() != "off" {
		t.Fatalf("group shares the caller's map: %+v", setting)
	}
	encoded, err := yaml.Marshal(struct {
		Group Group `yaml:"group,omitempty"`
	}{})
	if err != nil || string(encoded) != "{}\n" {
		t.Fatalf("empty group encoded as %q: %v", encoded, err)
	}
}

func TestLevelErrorsUseNamedLevels(t *testing.T) {
	for _, input := range []string{"''", "0", "[]", "{}"} {
		var level Level
		err := yaml.Unmarshal([]byte(input), &level)
		if err == nil {
			t.Fatalf("accepted %s", input)
		}
		if strings.Contains(err.Error(), "false") || strings.Contains(err.Error(), "null") {
			t.Fatalf("compatibility aliases advertised: %s", err)
		}
	}
}
