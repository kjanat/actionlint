package actionlint

import (
	"strings"

	"go.yaml.in/yaml/v4"
)

// configOrigins asks the YAML decoder to resolve each mapping, including merge precedence.
func configOrigins(node *yaml.Node, prefix string, origins map[string]ConfigOrigin, inputs map[*yaml.Node]configInput) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil
		}
		node = node.Content[0]
	}
	if node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	reset := inputs[node].reset
	if node.Tag == "!!null" && inputs[node].name != "" {
		reset = node
	}
	if reset != nil {
		// An input reset also explains why its descendants now use defaults.
		for pointer := range origins {
			if strings.HasPrefix(pointer, prefix+"/") {
				origins[pointer] = configNodeOrigin(reset, inputs)
			}
		}
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	values := map[string]*yaml.Node{}
	if inputs != nil {
		// Overlay resolution expands aliases and merges before attaching inputs.
		for i := 0; i < len(node.Content); i += 2 {
			values[node.Content[i].Value] = node.Content[i+1]
		}
	} else {
		var decoded map[string]yaml.Node
		if err := node.Decode(&decoded); err != nil {
			return err
		}
		for key, value := range decoded {
			values[key] = &value
		}
	}
	for key, value := range values {
		pointer := prefix + "/" + configPointerPart(key)
		if _, ok := origins[pointer]; !ok {
			continue
		}
		origins[pointer] = configNodeOrigin(value, inputs)
		if err := configOrigins(value, pointer, origins, inputs); err != nil {
			return err
		}
	}
	return nil
}

func configNodeOrigin(node *yaml.Node, inputs map[*yaml.Node]configInput) ConfigOrigin {
	origin := ConfigOrigin{Source: "config", State: "null"}
	if node == nil {
		return origin
	}
	origin.Line, origin.Column = node.Line, node.Column
	if node.Tag != "!!null" {
		origin.State = "value"
	}
	if input := inputs[node].name; input != "" {
		origin.Source, origin.Input = "input", input
	}
	return origin
}

func configPointerPart(s string) string { return strings.NewReplacer("~", "~0", "/", "~1").Replace(s) }

func configDefaultOrigins(values map[string]any, prefix string, origins map[string]ConfigOrigin) {
	for key, value := range values {
		pointer := prefix + "/" + configPointerPart(key)
		origins[pointer] = ConfigOrigin{Source: "default", State: "missing"}
		if nested, ok := value.(map[string]any); ok {
			configDefaultOrigins(nested, pointer, origins)
		}
	}
}

// effectiveConfig serializes the configuration schema after resolving defaults.
// Struct YAML tags remain the source of field names; policy marshalers own their
// public representation, so new config fields are included automatically.
func effectiveConfig(cfg *Config) (map[string]any, error) {
	resolved := *cfg
	if resolved.Tools.Shellcheck.Enabled == nil {
		resolved.Tools.Shellcheck.Enabled = new(true)
	}
	resolved.Policy.CacheCallUnrestricted = new(cfg.cachePolicyEnabled("cache-call-unrestricted"))
	resolved.Policy.CacheOperation = new(cfg.cachePolicyEnabled("cache-operation"))
	resolved.Policy.CacheWriteUntrusted = new(cfg.cachePolicyEnabled("cache-write-untrusted"))
	resolved.Policy.RequireCommitHash = new(cfg.RequiresCommitHash())
	if resolved.Policy.RequireJobTimeout == nil {
		resolved.Policy.RequireJobTimeout = &JobTimeoutPolicy{}
	}
	if resolved.Policy.RequirePermissions == nil {
		resolved.Policy.RequirePermissions = &PermissionsPolicy{}
	}
	if resolved.Policy.DisallowSuppressions == nil {
		resolved.Policy.DisallowSuppressions = &SuppressionsPolicy{}
	}
	var document yaml.Node
	if err := document.Encode(resolved); err != nil {
		return nil, err
	}
	// Unlike other lists, nil variables/secrets disables checking; [] permits none.
	for i := 0; i+1 < len(document.Content); i += 2 {
		key := document.Content[i].Value
		if (key == "config-variables" && cfg.ConfigVariables == nil) || (key == "config-secrets" && cfg.ConfigSecrets == nil) {
			document.Content[i+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
		}
	}
	var values map[string]any
	if err := document.Decode(&values); err != nil {
		return nil, err
	}
	return values, nil
}

// MarshalYAML preserves the public string representation of permission assumptions.
func (a DefaultPermissionsAssumption) MarshalYAML() (any, error) {
	if a == DefaultPermissionsAssumptionPermissive {
		return "permissive", nil
	}
	return "restricted", nil
}

// MarshalYAML serializes regular expressions as pattern strings.
func (pats IgnorePatterns) MarshalYAML() (any, error) {
	values := make([]string, len(pats))
	for i, pattern := range pats {
		values[i] = pattern.String()
	}
	return values, nil
}

// MarshalYAML emits false or the enabled policy's timeout bounds.
func (p *JobTimeoutPolicy) MarshalYAML() (any, error) {
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

// MarshalYAML emits false or the enabled policy's declaration scope.
func (p *PermissionsPolicy) MarshalYAML() (any, error) {
	if !p.Enabled() {
		return false, nil
	}
	return map[string]string{"scope": p.Scope()}, nil
}

// MarshalYAML emits false or the enabled policy's rule selection and reporting mode.
func (p *SuppressionsPolicy) MarshalYAML() (any, error) {
	if !p.Enabled() {
		return false, nil
	}
	value := map[string]any{"report": p.Report()}
	if rules := p.Rules(); len(rules) != 0 {
		value["rules"] = rules
	}
	return value, nil
}
