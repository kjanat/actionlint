package configuration

import (
	"fmt"
	"maps"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/configreport"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
)

func configOrigins(value any, prefix string, origins map[string]configreport.Origin, inputs map[string]document.Location) error {
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
		if source, exists := inputs[pointer]; exists && (source.Value.Line != 0 || source.Value.Column != 0 || source.Name != "") {
			origins[pointer] = configValueOrigin(child, source)
		}
		if err := configOrigins(child, pointer, origins, inputs); err != nil {
			return err
		}
	}
	return nil
}

func configValueOrigin(value any, input document.Location) configreport.Origin {
	origin := configreport.Origin{Source: "config", State: "null", File: input.File, Line: input.Value.Line, Column: input.Value.Column}
	if value != nil {
		origin.State = "value"
	}
	if input.Name != "" {
		origin.Source, origin.Input = "input", input.Name
	}
	return origin
}

func configPointerPart(s string) string { return strings.NewReplacer("~", "~0", "/", "~1").Replace(s) }

func configDefaultOrigins(values map[string]any, prefix string, origins map[string]configreport.Origin) {
	for key, value := range values {
		pointer := prefix + "/" + configPointerPart(key)
		origins[pointer] = configreport.Origin{Source: "default", State: "missing"}
		if nested, ok := value.(map[string]any); ok {
			configDefaultOrigins(nested, pointer, origins)
		}
	}
}

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

func (pats IgnorePatterns) CopyConfig() (any, error) {
	if pats == nil {
		return IgnorePatterns(nil), nil
	}
	next := make(IgnorePatterns, len(pats))
	for i, pattern := range pats {
		if pattern == nil {
			return nil, fmt.Errorf("ignore pattern %d is nil", i)
		}
		// Regexp.Copy performs the same value copy; its compiled program is immutable.
		copied := *pattern
		next[i] = &copied
	}
	return next, nil
}

func (pats IgnorePatterns) EncodeConfig() (any, error) {
	values := make([]string, len(pats))
	for i, pattern := range pats {
		values[i] = pattern.String()
	}
	return values, nil
}

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

func (p *PermissionsPolicy) EncodeConfig() (any, error) {
	if !p.Enabled() {
		return false, nil
	}
	return map[string]string{"scope": p.Scope()}, nil
}

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
