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
	level := Level("on")
	if !d.Recommended && !selected {
		level = "off"
	}
	if enabled, known := policyEnabled(cfg, d.Name); known {
		level = "off"
		if enabled {
			level = "on"
		}
	}
	var rules LintRulesConfig
	if cfg != nil {
		rules = cfg.Lint.Rules
	}
	level = presetLevel(rules.Preset, d, level)
	if p.Strict && !d.Nursery() {
		level = "on"
	}
	if p.Experimental != nil && *p.Experimental && d.Nursery() {
		level = "on"
	}
	group := rules.Groups()[d.Group]
	if d.Nursery() && d.Group == "nursery" && group.Preset() == "all" {
		level = "on"
	} else {
		level = presetLevel(group.Preset(), d, level)
	}
	if group.Level() != "" && group.Level() != "default" {
		level = group.Level()
	}
	// An explicit setting supplies the options and, unless it defers to the
	// default, the level.
	explicit, _ := group.Rule(d.Name)
	if explicit.Level() != "" && explicit.Level() != "default" {
		level = explicit.Level()
	}
	if d.Nursery() && p.Experimental != nil && !*p.Experimental {
		level = "off"
	}
	if slices.Contains(rules.Disable, d.Name) || cfg != nil && cfg.Lint.Enabled != nil && !*cfg.Lint.Enabled {
		level = "off"
	}
	return explicit.WithLevel(level)
}

func DiagnosticLevel(cfg *Config, name string) Level {
	if cfg != nil {
		if cfg.Lint.Enabled != nil && !*cfg.Lint.Enabled || slices.Contains(cfg.Lint.Rules.Disable, name) {
			return "off"
		}
		if level, ok := cfg.Lint.Rules.ResolvedLevel(name); ok {
			return level
		}
	}
	if d, ok := FindRule(name); ok {
		return SettingFor(cfg, d, Presets{}, false).Level()
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
	replacements := map[string]bool{}
	for _, descriptor := range registrations {
		replaced := false
		for i := range descriptors {
			if descriptors[i].Name == descriptor.Name && descriptors[i].Group == "external" {
				descriptors[i].Descriptor = descriptor
				replacements[descriptor.Name] = true
				replaced = true
				break
			}
		}
		if replaced {
			continue
		}
		descriptors = append(descriptors, Rule{Descriptor: descriptor, Group: "plugins"})
	}
	for _, d := range descriptors {
		setting := SettingFor(config, d, p, d.Group == "plugins" || replacements[d.Name])
		if replacements[d.Name] && config != nil {
			if explicit, ok := config.Lint.Rules.Plugins.Rule(d.Name); ok && explicit.Level() != "" && explicit.Level() != "default" {
				setting = setting.WithLevel(explicit.Level())
			}
			if slices.Contains(config.Lint.Rules.Disable, d.Name) || config.Lint.Enabled != nil && !*config.Lint.Enabled {
				setting = setting.WithLevel("off")
			}
		}
		resolved[d.Name] = setting.Level()
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
