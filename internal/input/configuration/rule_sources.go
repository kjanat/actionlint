package config

import (
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/configuration/address"
	configtree "actionlint.kjanat.dev/v2/internal/input/configuration/tree"
	"github.com/kjanat/go-yaml/v4"
)

// ruleSources stores captured documents before rule groups are resolved.
// Registration can change provider membership, so that resolution must use
// the original layers rather than the result of an earlier normalization.
type ruleSources struct {
	root      string
	documents map[string]*yaml.Node
	base      *yaml.Node
	overlays  []*yaml.Node
}

func (s *ruleSources) withOverlay(node *yaml.Node) *ruleSources {
	next := *s
	next.overlays = append(slices.Clone(s.overlays), node)
	return &next
}

func (ruleSet RuleSet) normalizeRuleDocument(node *yaml.Node) (*yaml.Node, error) {
	if node == nil || node.Kind != yaml.MappingNode {
		return node, nil
	}
	next := cloneMapping(node)
	removeField(next, "extends")
	if lint := field(node, "lint"); lint != nil {
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
	cache := map[string]*yaml.Node{}
	var load func(string) (*yaml.Node, error)
	load = func(path string) (*yaml.Node, error) {
		if node, found := cache[path]; found {
			return node, nil
		}
		node, found := sources.documents[path]
		if !found {
			return nil, fmt.Errorf("captured configuration source %q is missing", path)
		}
		var parents []string
		if extends := field(node, "extends"); extends != nil {
			if err := extends.Decode(&parents); err != nil {
				return nil, err
			}
		}
		var base *yaml.Node
		for _, parent := range parents {
			path, err := address.Resolve(path, parent)
			if err != nil {
				return nil, err
			}
			inherited, err := load(path)
			if err != nil {
				return nil, err
			}
			base = MergeDocuments(base, inherited, map[*yaml.Node]Input{})
		}
		normalized, err := ruleSet.normalizeRuleDocument(node)
		if err != nil {
			return nil, err
		}
		result := MergeDocuments(base, normalized, map[*yaml.Node]Input{})
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
		merged = MergeDocuments(merged, normalized, map[*yaml.Node]Input{})
	}
	if merged == nil {
		return config, nil
	}
	var decoded Config
	if err := ruleSet.DecodeNode(merged, &decoded); err != nil {
		return nil, err
	}
	next := *config
	next.Lint = decoded.Lint
	next.Lint.Rules.bindingSources = config.Lint.Rules.bindingSources
	// Overrides own their original lint nodes. ForFile applies the selected
	// overrides; reconstructing that list here would undo planning projections.
	return &next, nil
}

func captureRuleDocument(node *yaml.Node) (*yaml.Node, error) {
	if node == nil {
		return nil, nil
	}
	captured := mapping()
	for _, name := range []string{"extends", "lint"} {
		if value := field(node, name); value != nil {
			setField(captured, name, value)
		}
	}
	return configtree.Expand(captured, map[*yaml.Node]bool{})
}
