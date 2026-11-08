package config

import (
	"maps"
	"strings"
)

// configOrigins assigns source locations after inheritance and Action overrides.
func configOrigins(value any, prefix string, origins map[string]Origin, inputs map[string]Input) error {
	selected, found := inputs[prefix]
	if found {
		reset := selected.Reset
		if value == nil && selected.Name != "" {
			reset = &selected
		}
		if reset != nil {
			for pointer := range origins {
				if strings.HasPrefix(pointer, prefix+"/") {
					origins[pointer] = configValueOrigin(nil, *reset)
				}
			}
		}
	}
	for key, child := range members(value) {
		pointer := prefix + "/" + configPointerPart(key)
		if _, present := origins[pointer]; !present {
			continue
		}
		if source, exists := inputs[pointer]; exists && (source.Line != 0 || source.Column != 0 || source.Name != "") {
			origins[pointer] = configValueOrigin(child, source)
		}
		if err := configOrigins(child, pointer, origins, inputs); err != nil {
			return err
		}
	}
	return nil
}

func configValueOrigin(value any, input Input) Origin {
	origin := Origin{Source: "config", State: "null", File: input.File, Line: input.Line, Column: input.Column}
	if value != nil {
		origin.State = "value"
	}
	if input.Name != "" {
		origin.Source, origin.Input = "input", input.Name
	}
	return origin
}

func configPointerPart(s string) string { return strings.NewReplacer("~", "~0", "/", "~1").Replace(s) }

func configDefaultOrigins(values map[string]any, prefix string, origins map[string]Origin) {
	for key, value := range values {
		pointer := prefix + "/" + configPointerPart(key)
		origins[pointer] = Origin{Source: "default", State: "missing"}
		if nested, ok := value.(map[string]any); ok {
			configDefaultOrigins(nested, pointer, origins)
		}
	}
}

// EffectiveValues serializes the configuration schema after resolving defaults.
func (ruleSet RuleSet) EffectiveValues(cfg *Config) (map[string]any, error) {
	resolved := *cfg
	resolved.Tools = maps.Clone(cfg.Tools)
	if resolved.Tools == nil {
		resolved.Tools = ToolsConfig{}
	}
	for _, name := range toolProviderNames() {
		tool := resolved.Tools[name]
		if tool.Enabled == nil {
			tool.Enabled = new(true)
		}
		resolved.Tools[name] = tool
	}
	value, err := ruleSet.EncodeValue(&resolved)
	if err != nil {
		return nil, err
	}
	return members(value), nil
}

// EncodeConfig serializes regular expressions as pattern strings.
func (pats IgnorePatterns) EncodeConfig() (any, error) {
	values := make([]string, len(pats))
	for i, pattern := range pats {
		values[i] = pattern.String()
	}
	return values, nil
}

// EncodeConfig emits false or the enabled policy's timeout bounds.
func (p *JobTimeoutPolicy) EncodeConfig() (any, error) {
	if !p.Enabled() {
		return false, nil
	}
	bounds := map[string]float64{}
	if minimum, ok := p.MinMinutes(); ok {
		bounds["min-minutes"] = minimum
	}
	if maximum, ok := p.MaxMinutes(); ok {
		bounds["max-minutes"] = maximum
	}
	return bounds, nil
}

// EncodeConfig emits false or the enabled policy's declaration scope.
func (p *PermissionsPolicy) EncodeConfig() (any, error) {
	if !p.Enabled() {
		return false, nil
	}
	return map[string]string{"scope": p.Scope()}, nil
}

// EncodeConfig emits false or the enabled policy's rule selection and reporting mode.
func (p *SuppressionsPolicy) EncodeConfig() (any, error) {
	if !p.Enabled() {
		return false, nil
	}
	value := map[string]any{"report": p.Report()}
	if rules := p.Rules(); len(rules) != 0 {
		value["rules"] = rules
	}
	return value, nil
}
