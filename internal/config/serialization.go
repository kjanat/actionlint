package config

import (
	"github.com/kjanat/go-yaml/v4"
)

// UnmarshalYAML normalizes old documents at the boundary. Runtime values and
// subsequent serialization use the same canonical grouped configuration.
func (cfg *Config) UnmarshalYAML(node *yaml.Node) error {
	next, err := NormalizeForMerge(node)
	if err != nil {
		return err
	}
	type plain Config
	var decoded plain
	if err := next.Decode(&decoded); err != nil {
		return err
	}
	*cfg = Config(decoded)
	projectSettings(cfg)
	return nil
}

// MarshalYAML preserves programmatic callers using the Go API projections,
// while emitting only the current configuration syntax.
func (cfg *Config) MarshalYAML() (any, error) {
	type plain Config
	var root yaml.Node
	if err := root.Encode(plain(*cfg)); err != nil {
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
		rule, _ := FindRule(name)
		_, ok := cfg.Lint.Rules.Groups()[rule.Group].Rule(name)
		return ok
	}
	if !hasRule("runner-label") && cfg.SelfHostedRunner.Labels != nil {
		if err := add("self-hosted-runner", cfg.SelfHostedRunner); err != nil {
			return nil, err
		}
	}
	if !hasRule("expression-reference") {
		for _, entry := range []struct {
			key   string
			value []string
		}{{"config-variables", cfg.ConfigVariables}, {"config-secrets", cfg.ConfigSecrets}} {
			if entry.value != nil {
				if err := add(entry.key, entry.value); err != nil {
					return nil, err
				}
			}
		}
	}
	if !hasRule("workflow-call") && cfg.AssumeDefaultPermissions != DefaultPermissionsAssumptionUnset {
		if err := add("assume-default-permissions", cfg.AssumeDefaultPermissions); err != nil {
			return nil, err
		}
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
	next, _, err := migrateNode(&root)
	return next, err
}
