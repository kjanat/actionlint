package config

import (
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/configuration/address"
)

type ruleSources struct {
	root      string
	documents map[string]any
	base      any
	overlays  []any
}

func (s *ruleSources) withOverlay(value any) *ruleSources {
	next := *s
	next.overlays = append(slices.Clone(s.overlays), value)
	return &next
}

func (ruleSet RuleSet) normalizeRuleDocument(value any) (any, error) {
	if !isMapping(value) {
		return value, nil
	}
	next := cloneMapping(value)
	removeField(next, "extends")
	if lint := field(value, "lint"); lint != nil {
		lint, err := ruleSet.normalizeRuleLocations(lint)
		if err != nil {
			return nil, err
		}
		setField(next, "lint", ruleSet.expandLintShorthands(lint))
	}
	return next, nil
}

func (ruleSet RuleSet) capturedRuleConfig(config *Config) (*Config, error) {
	if config == nil || config.Lint.Rules.sources == nil {
		return config, nil
	}
	sources := config.Lint.Rules.sources
	merged := sources.base
	cache := map[string]any{}
	var load func(string) (any, error)
	load = func(path string) (any, error) {
		if value, found := cache[path]; found {
			return value, nil
		}
		value, found := sources.documents[path]
		if !found {
			return nil, fmt.Errorf("captured configuration source %q is missing", path)
		}
		var parents []string
		if extends := field(value, "extends"); extends != nil {
			if err := decodeValue(extends, &parents); err != nil {
				return nil, err
			}
		}
		var base any
		for _, parent := range parents {
			path, err := address.Resolve(path, parent)
			if err != nil {
				return nil, err
			}
			inherited, err := load(path)
			if err != nil {
				return nil, err
			}
			base = MergeDocuments(base, inherited)
		}
		normalized, err := ruleSet.normalizeRuleDocument(value)
		if err != nil {
			return nil, err
		}
		result := MergeDocuments(base, normalized)
		cache[path] = result
		return result, nil
	}
	if sources.root != "" {
		var err error
		merged, err = load(sources.root)
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		merged, err = ruleSet.normalizeRuleDocument(merged)
		if err != nil {
			return nil, err
		}
	}
	for _, overlay := range sources.overlays {
		normalized, err := ruleSet.normalizeRuleDocument(overlay)
		if err != nil {
			return nil, err
		}
		merged = MergeDocuments(merged, normalized)
	}
	if merged == nil {
		return config, nil
	}
	var decoded Config
	if err := ruleSet.DecodeValue(merged, &decoded); err != nil {
		return nil, err
	}
	next := *config
	next.Lint = decoded.Lint
	next.Lint.Rules.bindingSources = config.Lint.Rules.bindingSources
	return &next, nil
}

func captureRuleDocument(value any) any {
	if value == nil {
		return nil
	}
	captured := mapping()
	for _, name := range []string{"extends", "lint"} {
		if child := field(value, name); child != nil {
			setField(captured, name, cloneValue(child))
		}
	}
	return captured
}
