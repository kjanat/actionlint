package rules

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
)

func TestSettingCanonicalLevels(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			parsed, err := document.Parse("test.yaml", []byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			setting, err := parseSetting(parsed.Value)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := encodeTestConfig(setting)
			if err != nil {
				t.Fatal(err)
			}
			var normalized struct {
				Level string `yaml:"level"`
			}
			if err := decodeTestConfig(encoded, &normalized); err != nil {
				t.Fatal(err)
			}
			if normalized.Level != tc.want {
				t.Fatalf("got %s, want %s", normalized.Level, tc.want)
			}
		})
	}
}

// editingOptions mutates the values it receives to exercise ownership.
type editingOptions struct{ name string }

func (o *editingOptions) DecodeConfig(value any) error {
	object, ok := value.(map[string]any)
	if !ok {
		return errors.New("options are not a mapping")
	}
	name, ok := object["name"].(string)
	if !ok {
		return errors.New("name is not a string")
	}
	o.name = name
	object["name"] = "edited"
	return nil
}

func TestLoadOptionsDecodesPrivateCopy(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	if _, err := NewSetting("on", []string{"a"}); err == nil {
		t.Fatal("accepted sequence options")
	}
	setting, err := NewSetting("warn", nil)
	if err != nil || setting.HasOptions() || setting.Level() != "warn" {
		t.Fatalf("setting=%+v err=%v", setting, err)
	}
}

func TestNewSettingParsesJSONRawMessage(t *testing.T) {
	t.Parallel()
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
	for _, raw := range []json.RawMessage{json.RawMessage("[1]"), json.RawMessage("{"), json.RawMessage("max-count: 3"), json.RawMessage(`{"count":1,"count":2}`)} {
		if _, err := NewSetting("on", raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestGroupKeepsItsRulesAndOmitsWhenEmpty(t *testing.T) {
	t.Parallel()
	rules := map[string]Setting{"expression": {level: "off"}}
	group := NewGroup("", "warn", rules)
	rules["expression"] = Setting{level: "on"}
	if setting, ok := group.Rule("expression"); !ok || setting.Level() != "off" {
		t.Fatalf("group shares the caller's map: %+v", setting)
	}
	encoded, err := encodeTestConfig(struct {
		Group Group `yaml:"group,omitempty"`
	}{})
	if err != nil || string(encoded) != "{}\n" {
		t.Fatalf("empty group encoded as %q: %v", encoded, err)
	}
}

func TestLevelErrorsUseNamedLevels(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"''", "0", "[]", "{}"} {
		var level Level
		err := decodeTestConfig([]byte(input), &level)
		if err == nil {
			t.Fatalf("accepted %s", input)
		}
		if strings.Contains(err.Error(), "false") || strings.Contains(err.Error(), "null") {
			t.Fatalf("compatibility aliases advertised: %s", err)
		}
	}
}
