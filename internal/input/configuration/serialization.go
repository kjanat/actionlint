package config

import (
	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
)

type policyWire Policy

// Unmarshal reads YAML configuration and applies the shared migrations and validation.
func (ruleSet RuleSet) Unmarshal(source []byte, dst *Config) error {
	parsed, err := document.Parse("actionlint.yaml", source)
	if err != nil {
		return err
	}
	return ruleSet.DecodeValue(parsed.Value, dst)
}

// DecodeValue replaces dst only after the complete configuration has decoded.
func (ruleSet RuleSet) DecodeValue(value any, dst *Config) error {
	normalized, err := ruleSet.NormalizeForMerge(value)
	if err != nil {
		return err
	}
	var decoded Config
	if err := binding.DecodeKnown(rawValue(normalized), &decoded); err != nil {
		return err
	}
	if repository := field(normalized, "repository"); repository != nil && isNull(repository) {
		decoded.Repository = RepositoryConfig{reset: true}
	}
	ruleSet.projectSettings(&decoded)
	*dst = decoded
	return nil
}

// EncodeValue returns the current configuration syntax as ordinary Go values.
func (ruleSet RuleSet) EncodeValue(cfg *Config) (any, error) {
	if cfg == nil {
		return nil, nil
	}
	encoded, err := binding.Encode(*cfg)
	if err != nil {
		return nil, err
	}
	root := members(encoded)
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
		paths, err := binding.Encode(cfg.Paths)
		if err != nil {
			return nil, err
		}
		root["paths"] = paths
	}
	policy, err := binding.Encode(cfg.Policy)
	if err != nil {
		return nil, err
	}
	selected := mapping()
	for key, value := range members(policy) {
		if !hasRule(key) && !isNull(value) && (!isList(value) || len(elements(value)) != 0) {
			selected[key] = value
		}
	}
	if len(selected) != 0 {
		root["policy"] = selected
	}
	next, _, err := ruleSet.migrateNode(root)
	return next, err
}

// Marshal writes the current configuration in YAML format.
func (ruleSet RuleSet) Marshal(cfg *Config) ([]byte, error) {
	value, err := ruleSet.EncodeValue(cfg)
	if err != nil {
		return nil, err
	}
	return document.Marshal("actionlint.yaml", value)
}
