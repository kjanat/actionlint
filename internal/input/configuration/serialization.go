package config

import (
	"github.com/kjanat/go-yaml/v4"
)

// configWire owns YAML decoding independently of the runtime settings DTO.
type configWire struct {
	settings Config
	ruleSet  RuleSet
}

// policyWire validates legacy policy syntax before migration into rule settings.
type policyWire Policy

// Unmarshal decodes external YAML settings through the configuration wire adapter.
func (ruleSet RuleSet) Unmarshal(source []byte, dst *Config) error {
	wire := configWire{ruleSet: ruleSet}

	if err := yaml.Unmarshal(source, &wire); err != nil {
		return err
	}
	*dst = wire.settings
	return nil
}

// DecodeNode decodes one configuration value, including legacy normalization.
// Successful decoding replaces dst; errors leave it untouched.
func (ruleSet RuleSet) DecodeNode(node *yaml.Node, dst *Config) error {
	wire := configWire{ruleSet: ruleSet}

	if err := node.Decode(&wire); err != nil {
		return err
	}
	*dst = wire.settings
	return nil
}

// EncodeNode emits canonical syntax, including programmatic policy projections.
func (ruleSet RuleSet) EncodeNode(cfg *Config) (*yaml.Node, error) {
	if cfg == nil {
		var node yaml.Node
		if err := node.Encode(nil); err != nil {
			return nil, err
		}
		return &node, nil
	}
	var node yaml.Node
	if err := node.Encode(configWire{settings: *cfg, ruleSet: ruleSet}); err != nil {
		return nil, err
	}
	return &node, nil
}

// Marshal encodes settings through the canonical configuration wire contract.
func (ruleSet RuleSet) Marshal(cfg *Config) ([]byte, error) {
	node, err := ruleSet.EncodeNode(cfg)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(node)
}

// UnmarshalYAML normalizes old documents at the boundary. Runtime values and
// subsequent serialization use the same canonical grouped configuration.
func (wire *configWire) UnmarshalYAML(node *yaml.Node) error {
	ruleSet := wire.ruleSet

	next, err := ruleSet.NormalizeForMerge(node)
	if err != nil {
		return err
	}
	var decoded Config
	if err := next.Decode(&decoded); err != nil {
		return err
	}
	if repository := field(next, "repository"); repository != nil && repository.ShortTag() == "!!null" {
		decoded.Repository = RepositoryConfig{reset: true}
	}
	ruleSet.projectSettings(&decoded)
	wire.settings = decoded
	return nil
}

// MarshalYAML includes programmatically selected policy fields while emitting
// the current configuration syntax.
func (wire configWire) MarshalYAML() (any, error) {
	ruleSet := wire.ruleSet

	cfg := wire.settings
	var root yaml.Node
	if err := root.Encode(cfg); err != nil {
		return nil, err
	}
	add := func(key string, value any) error {
		var node yaml.Node
		if err := node.Encode(value); err != nil {
			return err
		}
		setField(&root, key, &node)
		return nil
	}
	hasRule := func(name string) bool {
		descriptor, known := ruleSet.FindRule(name)
		if !known {
			return false
		}
		if _, ok := ruleSet.canonicalSetting(cfg.Lint.Rules.Groups()[descriptor.Category], descriptor.Name); ok {
			return true
		}
		_, ok := ruleSet.canonicalSetting(cfg.Lint.Rules.LegacyGroups()[ruleSet.LegacyGroup(descriptor.Name)], descriptor.Name)
		return ok
	}
	if cfg.Lint.MessageIgnores == nil && cfg.Paths != nil {
		if err := add("paths", cfg.Paths); err != nil {
			return nil, err
		}
	}
	var policy yaml.Node
	if err := policy.Encode(cfg.Policy); err != nil {
		return nil, err
	}
	selected := mapping()
	for i := 0; i < len(policy.Content); i += 2 {
		key, value := policy.Content[i], policy.Content[i+1]
		if !hasRule(key.Value) && value.ShortTag() != "!!null" && (value.Kind != yaml.SequenceNode || len(value.Content) != 0) {
			selected.Content = append(selected.Content, key, value)
		}
	}
	if len(selected.Content) > 0 {
		setField(&root, "policy", selected)
	}
	next, _, err := ruleSet.migrateNode(&root)
	return next, err
}
