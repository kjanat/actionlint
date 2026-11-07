package config

import (
	"fmt"

	lintconfig "actionlint.kjanat.dev/v2/internal/input/configuration/rules"
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
// selects the rule's default. See [lintconfig.NewSetting] for the accepted
// options values. ValidateRuleNames checks the options against the rule.
func NewSetting(level Level, options any) (Setting, error) {
	return lintconfig.NewSetting(level, options)
}

// NewGroup returns a rule group with a preset, a group level and per-rule settings.
func NewGroup(preset Preset, level Level, rules map[string]Setting) Group {
	return lintconfig.NewGroup(preset, level, rules)
}

// loadOptions decodes the options of s into dst and reports whether it did.
// ResolveRules validates options before policy projection calls this helper.
func loadOptions(s Setting, dst any) bool {
	loaded, err := s.LoadOptions(dst)
	return loaded && err == nil
}

// RuleOptions decodes options from the rule's semantic category. It returns
// nil when the rule has no options. Each call returns a new map.
func (c *LintRulesConfig) RuleOptions(name string) (map[string]any, error) {
	ruleSet := c.ruleSet

	name = ruleSet.ResolveAlias(name)
	category, bound := c.boundCategories[name]
	if !bound {
		if descriptor, known := ruleSet.FindRule(name); known {
			category = descriptor.Category
		}
	}
	setting, found := ruleSet.canonicalSetting(c.Groups()[category], name)
	if boundSetting, bound := c.boundSettings[name]; bound {
		setting, found = boundSetting, true
	}
	if !found {
		// Legacy registrations remain readable until migration has metadata.
		setting, _ = ruleSet.canonicalSetting(c.Plugins, name)
	}
	var options map[string]any
	if _, err := setting.LoadOptions(&options); err != nil {
		return nil, fmt.Errorf("options for analyzer %q: %w", name, err)
	}
	return options, nil
}

func (ruleSet RuleSet) canonicalSetting(group Group, name string) (Setting, bool) {
	if setting, found := group.Rule(name); found {
		return setting, true
	}
	for alias, setting := range group.Rules() {
		if ruleSet.ResolveAlias(alias) == name {
			return setting, true
		}
	}
	return Setting{}, false
}

func (c *LintRulesConfig) Groups() map[string]Group {
	return map[string]Group{"correctness": c.Correctness, "suspicious": c.Suspicious, "security": c.Security, "conventions": c.Conventions}
}

// LegacyGroups exposes decode-only controls needed for metadata-aware migration.
func (c *LintRulesConfig) LegacyGroups() map[string]Group {
	return map[string]Group{"style": c.Style, "policy": c.Policy, "external": c.External, "nursery": c.Nursery, "plugins": c.Plugins}
}

// LegacyGroup identifies a built-in rule's configuration group before semantic
// categories replaced provider, maturity and policy groups.
func (ruleSet RuleSet) LegacyGroup(name string) string {
	name = ruleSet.ResolveAlias(name)
	if _, bundled := toolProviders()[name]; bundled {
		return "external"
	}
	switch name {
	case "empty-triggers", "empty-job", "empty-run":
		return "style"
	case "uncancellable-condition":
		return "correctness"
	case "unpinned-reference", "job-timeout", "explicit-permissions", "missing-action-metadata", "forbidden-suppression":
		return "policy"
	default:
		if descriptor, known := ruleSet.FindRule(name); known {
			return descriptor.Category
		}
		return ""
	}
}
