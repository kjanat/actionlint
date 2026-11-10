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

func settingWithOptions(options *jsonschema.Schema) *jsonschema.Schema {
	properties := jsonschema.NewProperties()
	properties.Set("options", options)
	return &jsonschema.Schema{AllOf: []*jsonschema.Schema{
		definitionRef("plugin-setting"),
		{Properties: properties, AdditionalProperties: jsonschema.TrueSchema},
	}}
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
	options, _ := setting.Properties.Get("options")
	options.AdditionalProperties = jsonschema.TrueSchema
	plain := settingWithOptions(&jsonschema.Schema{Type: "null", Description: "Reset rule options to their defaults."})
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
	knownValues := make([]any, 0, len(names))
	for _, choice := range names {
		knownValues = append(knownValues, choice.Const)
	}
	return jsonschema.Definitions{
		"nul-free-string":        {Type: "string", Not: &jsonschema.Schema{Pattern: `[\x00]`}},
		"single-line-path":       {Type: "string", MinLength: new(uint64(1)), Not: &jsonschema.Schema{Pattern: `[\x00\r\n]`}},
		"local-action-directory": {Type: "string", Pattern: `^(\./|\$/)`, Not: &jsonschema.Schema{Pattern: `[\\:\x00\r\n]|\$\{\{`}},
		"known-rule-name":        {AnyOf: names},
		"known-rule-name-values": {Enum: knownValues},
		"rule-name": {
			Description: "Built-in rule ID or registered analyzer name. actionlint validates names against the loaded analyzers.",
			OneOf: []*jsonschema.Schema{
				definitionRef("known-rule-name"),
				{Type: "string", MinLength: new(uint64(1)), Not: definitionRef("known-rule-name-values"), Extras: map[string]any{"doNotSuggest": true}},
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
