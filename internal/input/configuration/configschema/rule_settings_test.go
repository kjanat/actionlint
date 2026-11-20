package configschema

import (
	"encoding/json"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/toolsettings"
)

func TestActionDiagnosticsHaveSeparateSchemaSettings(t *testing.T) {
	t.Parallel()
	schema := compile(t)
	for _, name := range []string{"missing-file", "unknown-input", "required-input", "deprecated-input", "invalid-action-reference", "action-runtime", "action-branding", "action-input-deprecation", "invalid-dockerfile", "action-metadata-read"} {
		setting := object{"lint": object{"rules": object{"correctness": object{name: "warn"}}}}
		for _, input := range []object{setting, {"overrides": []object{{"includes": []string{"**/action.yml"}, "lint": setting["lint"]}}}, {"ignores": []object{{"rule": name, "file": "**/action.yml"}}}} {
			data, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(data); err != nil {
				t.Errorf("rule %s rejected: %s: %v", name, data, err)
			}
		}
	}
	data, err := Document(moduleRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Definitions map[string]struct {
			Choices []struct {
				Name string `json:"const"`
			} `json:"anyOf"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, choice := range document.Definitions["known-rule-name"].Choices {
		if choice.Name == "action" {
			t.Fatal("removed action catch-all remains in the rule catalog")
		}
	}
}

func TestInstalledRuleNamesExtendSharedSelectors(t *testing.T) {
	t.Parallel()
	custom := installed(false)
	custom.Rule.Name = "custom-check"
	bundled := installed(false)
	bundled.Rule.Name, bundled.Rule.Category = "shellcheck", "correctness"
	data, err := Document(moduleRoot, []toolsettings.Provider{custom, bundled})
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Definitions map[string]struct {
			Choices []struct {
				Name string `json:"const"`
			} `json:"anyOf"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, choice := range document.Definitions["known-rule-name"].Choices {
		names = append(names, choice.Name)
	}
	if !slices.IsSorted(names) || !slices.Contains(names, "custom-check") {
		t.Fatalf("installed rule missing or unordered: %v", names)
	}
	for _, name := range []string{"shellcheck", "custom-check"} {
		count := 0
		for _, actual := range names {
			if actual == name {
				count++
			}
		}
		if count != 1 {
			t.Errorf("rule %s occurs %d times", name, count)
		}
	}
	schema := compile(t, custom, bundled)
	for _, input := range []string{
		`{"ignores":[{"rule":"custom-check","file":"**"}]}`,
		`{"lint":{"rules":{"disable":["custom-check","unpublished-check"]}}}`,
		`{"lint":{"rules":{"conventions":{"forbidden-suppression":{"level":"on","options":{"rules":["custom-check","unpublished-check"]}}}}}}`,
	} {
		if err := schema.Validate([]byte(input)); err != nil {
			t.Errorf("valid selector rejected: %s: %v", input, err)
		}
	}
}

func TestInstalledRuleUsesSharedSettingContract(t *testing.T) {
	t.Parallel()
	for _, typedOptions := range []bool{false, true} {
		provider := installed(false)
		provider.Rule.Name = "custom-check"
		if typedOptions {
			provider.Rule.OptionsSchema = json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1}},"required":["limit"],"additionalProperties":false}`)
		}
		schema := compile(t, provider)
		for _, override := range []bool{false, true} {
			validate := func(value any) error {
				entry := object{"lint": object{"rules": object{"conventions": object{"custom-check": value}}}}
				if override {
					entry["includes"] = []string{"**"}
					entry = object{"overrides": []any{entry}}
				}
				data, err := json.Marshal(entry)
				if err != nil {
					t.Fatal(err)
				}
				return schema.Validate(data)
			}
			for _, valid := range []any{
				"default", "off", "on", "info", "warn", "error", true, false, nil,
				object{"level": true},
				object{"level": false},
				object{"level": nil},
				object{"level": "warn", "options": nil},
			} {
				if err := validate(valid); err != nil {
					t.Errorf("typed=%t override=%t: valid setting %#v rejected: %v", typedOptions, override, valid, err)
				}
			}
			for _, invalid := range []any{
				"unknown", "all", 42,
				object{},
				object{"level": "bogus"},
				object{"level": "warn", "unexpected": true},
				object{"level": "warn", "options": []any{}},
			} {
				if err := validate(invalid); err == nil {
					t.Errorf("typed=%t override=%t: invalid setting %#v accepted", typedOptions, override, invalid)
				}
			}
			validOptions := object{"level": "warn", "options": object{"limit": 2}}
			if err := validate(validOptions); (err == nil) != typedOptions {
				t.Errorf("typed=%t override=%t: options schema did not control acceptance: %v", typedOptions, override, err)
			}
			for _, options := range []any{object{"limit": 0}, object{"limit": 2, "unknown": true}} {
				if err := validate(object{"level": "warn", "options": options}); err == nil {
					t.Errorf("typed=%t override=%t: invalid options %#v accepted", typedOptions, override, options)
				}
			}
		}
	}
}
