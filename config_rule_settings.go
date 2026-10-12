package actionlint

import (
	"slices"

	"actionlint.kjanat.dev/internal/lintconfig"
)

// RuleLevel controls the severity of a diagnostic.
type RuleLevel = lintconfig.Level

// RulePreset selects a rule baseline.
type RulePreset = lintconfig.Preset

// RuleSetting provides a level and typed per-rule options.
type RuleSetting = lintconfig.Setting

// RuleGroupConfig selects rules within a concern.
type RuleGroupConfig = lintconfig.Group

func (c LintRulesConfig) groups() map[string]RuleGroupConfig {
	return map[string]RuleGroupConfig{"correctness": c.Correctness, "suspicious": c.Suspicious, "security": c.Security, "policy": c.Policy, "external": c.External, "nursery": c.Nursery}
}

func presetLevel(p RulePreset, d ruleDescriptor, fallback RuleLevel) RuleLevel {
	switch p {
	case "none":
		return "off"
	case "all":
		if !d.isNursery() {
			return "on"
		}
		return fallback
	case "recommended":
		if d.Recommended {
			return "on"
		}
		return "off"
	default:
		return fallback
	}
}

func (cfg *Config) ruleSetting(d ruleDescriptor, p RulePresets) RuleSetting {
	setting := RuleSetting{Level: "on"}
	if !d.Recommended {
		setting.Level = "off"
	}
	if d.configure != nil && d.enabled != nil {
		setting.Level = "off"
		if d.enabled(ruleContext{config: cfg}) {
			setting.Level = "on"
		}
	}
	var rules LintRulesConfig
	if cfg != nil {
		rules = cfg.Lint.Rules
	}
	setting.Level = presetLevel(rules.Preset, d, setting.Level)
	if p.Strict && !d.isNursery() {
		setting.Level = "on"
	}
	if p.Experimental != nil && *p.Experimental && d.isNursery() {
		setting.Level = "on"
	}
	group := rules.groups()[d.Category]
	if d.isNursery() && group.Preset == "all" {
		setting.Level = "on"
	} else {
		setting.Level = presetLevel(group.Preset, d, setting.Level)
	}
	if group.Level != "" && group.Level != "default" {
		setting.Level = group.Level
	}
	if explicit, ok := group.Rules[d.Name]; ok {
		if explicit.Level == "" || explicit.Level == "default" {
			// The default level retains the applicable preset/group baseline.
			explicit.Level = setting.Level
		}
		setting = explicit
	}
	if d.isNursery() && p.Experimental != nil && !*p.Experimental {
		setting.Level = "off"
	}
	if slices.Contains(rules.Disable, d.Name) || cfg != nil && cfg.Lint.Enabled != nil && !*cfg.Lint.Enabled {
		setting.Level = "off"
	}
	return setting
}

func (cfg *Config) diagnosticLevel(name string) RuleLevel {
	if cfg != nil {
		if level, ok := cfg.Lint.Rules.resolved[name]; ok {
			return level
		}
	}
	if d, ok := findRuleDescriptor(name); ok {
		return cfg.ruleSetting(d, RulePresets{}).Level
	}
	return "on" // Caller-provided rules retain their own behavior.
}
