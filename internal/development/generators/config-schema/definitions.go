package main

import (
	"encoding/json"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	actionlint "actionlint.kjanat.dev/v2/internal/input/configuration"
	lintconfig "actionlint.kjanat.dev/v2/internal/input/configuration/rules"
	"actionlint.kjanat.dev/v2/internal/input/configuration/schema"

	"github.com/invopop/jsonschema"
)

func definitionRef(name string) *jsonschema.Schema {
	return &jsonschema.Schema{Ref: "#/$defs/" + name}
}

func enumValues[T ~string](values []T) []any {
	result := make([]any, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}

func levelSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		OneOf: []*jsonschema.Schema{
			{Type: "string", Enum: enumValues(lintconfig.Levels())},
			{Type: "boolean"},
			{Type: "null", Extras: map[string]any{"doNotSuggest": true}},
		},
	}
}

func sharedDefinitions(reflectMapping func(any) *jsonschema.Schema) (jsonschema.Definitions, error) {
	setting := reflectMapping(struct {
		Level   actionlint.Level `yaml:"level" jsonschema:"required,description=Diagnostic level for this rule."`
		Options map[string]any   `yaml:"options,omitempty" jsonschema:"nullable,description=Options validated against the rule's schema; null resets its defaults."`
	}{})
	plain := definitionRef("plugin-setting")
	plain.Properties = jsonschema.NewProperties()
	plain.Properties.Set("options", &jsonschema.Schema{Type: "null", Description: "Reset rule options to their defaults."})
	configuredRules := checking.ConfigurationRules().Rules()
	descriptors := make([]rule.Descriptor, 0, len(configuredRules))
	for _, entry := range configuredRules {
		descriptors = append(descriptors, entry.Descriptor)
	}
	choices, err := json.Marshal(schema.RuleNameChoices(descriptors))
	if err != nil {
		return nil, err
	}
	var names []*jsonschema.Schema
	if err := json.Unmarshal(choices, &names); err != nil {
		return nil, err
	}
	return jsonschema.Definitions{
		"known-rule-name": {AnyOf: names},
		"rule-name": {
			Description: "Built-in rule ID or registered analyzer name. actionlint validates names against the loaded analyzers.",
			AnyOf: []*jsonschema.Schema{
				definitionRef("known-rule-name"),
				{Type: "string", MinLength: new(uint64(1)), Extras: map[string]any{"doNotSuggest": true}},
			},
		},
		"level": levelSchema(),
		"group-level": {AnyOf: []*jsonschema.Schema{
			definitionRef("level"), {Type: "string", Enum: []any{"all"}},
		}},
		"preset":         {Type: "string", Enum: enumValues(lintconfig.Presets()), Description: "Baseline rule selection: recommended, all stable rules, or none."},
		"plugin-setting": {OneOf: []*jsonschema.Schema{definitionRef("level"), setting}},
		"rule-setting":   plain,
		"backend":        backendSchema(reflectMapping),
	}, nil
}
