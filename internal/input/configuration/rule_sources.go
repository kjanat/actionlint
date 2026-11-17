package configuration

import (
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
)

// ruleSources keeps the extends and lint settings of every loaded file, the
// encoded base configuration and the command-line overlays, so lint settings
// can be merged again once plugin rules are registered. Rule settings are
// normalized per file before merging, and a plugin rule's category is unknown
// until its plugin loads, so the merged tree alone cannot be rebound. root
// names the selected file; files holds each file's rule settings by
// normalized address, for document.Load to merge without reading.
type ruleSources struct {
	root     string
	files    *document.Files
	base     any
	overlays []any
}

func (s *ruleSources) withOverlay(value any) *ruleSources {
	next := *s
	next.overlays = append(slices.Clone(s.overlays), value)
	return &next
}

// normalizeRuleLint gives the lint settings in value one merge key per rule.
// Other fields, including extends, are kept.
func (ruleSet RuleSet) normalizeRuleLint(value any) (any, error) {
	if !isMapping(value) {
		return value, nil
	}
	next := cloneMapping(value)
	if lint := field(value, "lint"); lint != nil {
		lint, err := ruleSet.normalizeRuleLocations(lint)
		if err != nil {
			return nil, err
		}
		setField(next, "lint", ruleSet.expandLintShorthands(lint))
	}
	return next, nil
}

// normalizeRuleDocument normalizes the lint settings of a document that has
// no inheritance to follow.
func (ruleSet RuleSet) normalizeRuleDocument(value any) (any, error) {
	next, err := ruleSet.normalizeRuleLint(value)
	if err != nil || !isMapping(next) {
		return next, err
	}
	removeField(next, "extends")
	return next, nil
}

// capturedRuleConfig decodes the lint settings again with ruleSet, which
// includes the rules of loaded plugins. It merges the captured files with
// document.Load, the loader that merged them first. A decoded result holds no
// rule sources, so binding it again returns it unchanged.
func (ruleSet RuleSet) capturedRuleConfig(config *Config) (*Config, error) {
	if config == nil || config.Lint.Rules.sources == nil {
		return config, nil
	}
	sources := config.Lint.Rules.sources
	var merged *document.Node
	if sources.root != "" {
		// Report normalization failures as the settings decoder does, without
		// the source-loading context that Load adds.
		var normalizeErr error
		loaded, err := document.LoadConfigDocument(sources.root, sources.files, func(path string) ([]byte, error) {
			return nil, fmt.Errorf("captured configuration source %q is missing", path)
		}, func(_ string, source *document.Node) (*document.Node, error) {
			normalized, err := ruleSet.normalizeRuleLint(source.Value())
			normalizeErr = err
			return document.Capture(normalized, nil), err
		}, MergePlan())
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		if err != nil {
			return nil, err
		}
		merged = loaded.Root
	} else {
		normalized, err := ruleSet.normalizeRuleDocument(sources.base)
		if err != nil {
			return nil, err
		}
		merged = document.Capture(normalized, nil)
	}
	for _, overlay := range sources.overlays {
		normalized, err := ruleSet.normalizeRuleDocument(overlay)
		if err != nil {
			return nil, err
		}
		merged = document.Merge(merged, document.Capture(normalized, nil), MergePlan())
	}
	if merged.Value() == nil {
		return config, nil
	}
	var decoded Config
	if err := ruleSet.DecodeValue(merged.Value(), &decoded); err != nil {
		return nil, err
	}
	next := *config
	next.Lint = decoded.Lint
	next.Lint.locations = config.Lint.locations
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
