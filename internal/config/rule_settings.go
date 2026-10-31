package config

import (
	"fmt"

	lintconfig "actionlint.kjanat.dev/v2/internal/config/rules"
)

// Level controls the severity of a diagnostic.
type Level = lintconfig.Level

// Preset selects a rule baseline.
type Preset = lintconfig.Preset

// Setting provides a level and typed per-rule options.
type Setting = lintconfig.Setting

// Group selects rules within a concern.
type Group = lintconfig.Group

// NewSetting returns a rule setting with a level and options. An empty level
// selects the rule's default; options must encode to a YAML mapping.
func NewSetting(level Level, options any) (Setting, error) {
	return lintconfig.NewSetting(level, options)
}

// NewGroup returns a rule group with a preset, a group level and per-rule settings.
func NewGroup(preset Preset, level Level, rules map[string]Setting) Group {
	return lintconfig.NewGroup(preset, level, rules)
}

// loadOptions decodes the options of s into dst and reports whether it did.
// Parsing already checked options against the rule's type, so a decode error
// can only come from a setting built in Go with options of another type. Such
// options are ignored, and the rule keeps its default options.
func loadOptions(s Setting, dst any) bool {
	loaded, err := s.LoadOptions(dst)
	return loaded && err == nil
}

// PluginOptions decodes the options set for the registered analyzer name in
// lint.rules.plugins. It returns nil when the analyzer has no options. Each
// call returns a new map.
func (c *LintRulesConfig) PluginOptions(name string) (map[string]any, error) {
	setting, _ := c.Plugins.Rule(name)
	var options map[string]any
	if _, err := setting.LoadOptions(&options); err != nil {
		return nil, fmt.Errorf("options for analyzer %q: %w", name, err)
	}
	return options, nil
}

func (c *LintRulesConfig) Groups() map[string]Group {
	return map[string]Group{"correctness": c.Correctness, "suspicious": c.Suspicious, "style": c.Style, "security": c.Security, "policy": c.Policy, "external": c.External, "nursery": c.Nursery, "plugins": c.Plugins}
}
