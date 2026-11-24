package configuration

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/input/configuration/ruleconfig"
)

func loadOptions(s ruleconfig.Setting, dst any) bool {
	loaded, err := s.LoadOptions(dst)
	return loaded && err == nil
}

// RuleOptions returns nil when the rule has no options and a new map on each call.
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
		setting, found = boundSetting.Setting, true
	}
	if !found {
		setting, _ = ruleSet.canonicalSetting(c.Plugins, name)
	}
	var options map[string]any
	if _, err := setting.LoadOptions(&options); err != nil {
		return nil, fmt.Errorf("options for analyzer %q: %w", name, err)
	}
	return options, nil
}

func (ruleSet RuleSet) canonicalSetting(group ruleconfig.Group, name string) (ruleconfig.Setting, bool) {
	if setting, found := group.Rule(name); found {
		return setting, true
	}
	for alias, setting := range group.Rules() {
		if ruleSet.ResolveAlias(alias) == name {
			return setting, true
		}
	}
	return ruleconfig.Setting{}, false
}

func (c *LintRulesConfig) Groups() map[string]ruleconfig.Group {
	return map[string]ruleconfig.Group{"correctness": c.Correctness, "suspicious": c.Suspicious, "security": c.Security, "conventions": c.Conventions}
}

func (c *LintRulesConfig) LegacyGroups() map[string]ruleconfig.Group {
	return map[string]ruleconfig.Group{"style": c.Style, "policy": c.Policy, "external": c.External, "nursery": c.Nursery, "plugins": c.Plugins}
}

func (ruleSet RuleSet) LegacyGroup(name string) string {
	name = ruleSet.ResolveAlias(name)
	if _, bundled := toolProviders()[name]; bundled {
		return "external"
	}
	descriptor, known := ruleSet.FindRule(name)
	switch {
	case !known:
		return ""
	case descriptor.LegacyGroup != "":
		return descriptor.LegacyGroup
	default:
		return descriptor.Category
	}
}
