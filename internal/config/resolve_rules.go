package config

import (
	"path/filepath"
	"slices"

	"actionlint.kjanat.dev/v2/rule"
)

func presetLevel(p Preset, d Rule, fallback Level) Level {
	switch p {
	case "none":
		return "off"
	case "all":
		if !d.Nursery() {
			return "on"
		}
		return "off"
	case "recommended":
		if d.Recommended {
			return "on"
		}
		return "off"
	default:
		return fallback
	}
}

func SettingFor(cfg *Config, d Rule, p Presets, selected bool) Setting {
	setting := Setting{Level: "on"}
	if !d.Recommended && !selected {
		setting.Level = "off"
	}
	if enabled, known := policyEnabled(cfg, d.Name); known {
		setting.Level = "off"
		if enabled {
			setting.Level = "on"
		}
	}
	var rules LintRulesConfig
	if cfg != nil {
		rules = cfg.Lint.Rules
	}
	setting.Level = presetLevel(rules.Preset, d, setting.Level)
	if p.Strict && !d.Nursery() {
		setting.Level = "on"
	}
	if p.Experimental != nil && *p.Experimental && d.Nursery() {
		setting.Level = "on"
	}
	group := rules.Groups()[d.Group]
	if d.Nursery() && d.Group == "nursery" && group.Preset == "all" {
		setting.Level = "on"
	} else {
		setting.Level = presetLevel(group.Preset, d, setting.Level)
	}
	if group.Level != "" && group.Level != "default" {
		setting.Level = group.Level
	}
	if explicit, ok := group.Rules[d.Name]; ok {
		if explicit.Level == "" || explicit.Level == "default" {
			explicit.Level = setting.Level
		}
		setting = explicit
	}
	if d.Nursery() && p.Experimental != nil && !*p.Experimental {
		setting.Level = "off"
	}
	if slices.Contains(rules.Disable, d.Name) || cfg != nil && cfg.Lint.Enabled != nil && !*cfg.Lint.Enabled {
		setting.Level = "off"
	}
	return setting
}

func DiagnosticLevel(cfg *Config, name string) Level {
	if cfg != nil {
		if level, ok := cfg.Lint.Rules.ResolvedLevel(name); ok {
			return level
		}
	}
	if d, ok := FindRule(name); ok {
		return SettingFor(cfg, d, Presets{}, false).Level
	}
	return "on"
}

// ResolveRules projects presets and explicit rule settings into an effective configuration.
// Registered descriptors participate in selection without loading analyzer implementations.
func ResolveRules(p Presets, config *Config, registrations ...rule.Descriptor) *Config {
	if config == nil && !p.Strict && p.Experimental == nil && len(registrations) == 0 {
		return nil
	}
	next := Config{}
	if config != nil {
		next = *config
	}
	resolved := make(map[string]Level)
	next.Lint.Rules.SetResolved(resolved)
	descriptors := Rules()
	for _, descriptor := range registrations {
		descriptors = append(descriptors, Rule{Descriptor: descriptor, Group: "plugins"})
	}
	for _, d := range descriptors {
		setting := SettingFor(config, d, p, d.Group == "plugins")
		resolved[d.Name] = setting.Level
		if d.Configure != nil {
			d.Configure(&next, setting)
		}
	}
	return &next
}

func policyEnabled(cfg *Config, name string) (bool, bool) {
	switch name {
	case "disallow-suppressions":
		return cfg != nil && cfg.Policy.DisallowSuppressions.Enabled(), true
	case "require-commit-hash":
		return cfg.RequiresCommitHash(), true
	case "require-job-timeout":
		return cfg.RequiresJobTimeout().Enabled(), true
	case "require-permissions":
		return cfg.RequiresPermissions().Enabled(), true
	case "required-actions":
		return len(cfg.RequiredActions()) > 0, true
	case "cache-write-untrusted", "cache-call-unrestricted", "cache-operation":
		return cfg.CachePolicyEnabled(name), true
	default:
		return false, false
	}
}

func ForDocument(config *Config, path, root string) (*Config, error) {
	if filepath.Base(path) == "action.yml" || filepath.Base(path) == "action.yaml" {
		return ForAction(config, path, root)
	}
	return ForFile(config, path, root)
}
