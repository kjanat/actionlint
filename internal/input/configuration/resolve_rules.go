package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests/configschema"
)

func presetLevel(p Preset, d Rule, fallback Level) Level {
	switch p {
	case "none":
		return "off"
	case "all":
		if !d.Experimental() {
			return "on"
		}
		return "off"
	case "recommended":
		if d.Recommended && !d.Experimental() {
			return defaultLevel(d)
		}
		return "off"
	default:
		return fallback
	}
}

func defaultLevel(d Rule) Level {
	if d.DefaultLevel != "" {
		return d.DefaultLevel
	}
	return "on"
}

func (ruleSet RuleSet) SettingFor(cfg *Config, d Rule, p Presets, selected bool) Setting {
	level := defaultLevel(d)
	if d.Experimental() || !d.Recommended && !selected {
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
	if p.Strict && !d.Experimental() {
		level = "on"
		if d.DefaultLevel != "" {
			level = "error"
		}
	}
	if p.Experimental != nil && *p.Experimental && d.Experimental() {
		level = "on"
	}
	// Retained legacy controls apply only to their original membership. The
	// semantic category then supplies the current baseline and exception.
	groups := []Group{}
	legacyCategory := ruleSet.LegacyGroup(d.Name)
	if legacy, removed := rules.LegacyGroups()[legacyCategory]; removed {
		groups = append(groups, legacy)
	} else if legacyCategory != d.Category {
		legacy := rules.Groups()[legacyCategory]
		if _, explicit := ruleSet.canonicalSetting(legacy, d.Name); explicit {
			groups = append(groups, legacy)
		}
	}
	if selected {
		groups = append(groups, rules.Plugins)
	}
	if d.Experimental() {
		// Decode-only nursery controls retain their historical opt-in. A
		// migration expands that selection into individual category entries.
		if rules.Nursery.Preset() == "all" {
			level = "on"
		}
		if legacyLevel := rules.Nursery.Level(); legacyLevel != "" && legacyLevel != "default" {
			level = legacyLevel
		}
		groups = append(groups, rules.Nursery)
	}
	groups = append(groups, rules.Groups()[d.Category])
	var explicit Setting
	var explicitLevel Level
	for _, group := range groups {
		// Category baselines select stable checks only. Nursery checks need
		// an individual enable or the invocation's experimental switch.
		if !d.Experimental() {
			level = presetLevel(group.Preset(), d, level)
		} else if group.Preset() == "none" {
			level = "off"
		}
		if group.Level() != "" && group.Level() != "default" && (!d.Experimental() || level != "off" || group.Level() == "off") {
			level = group.Level()
		}
		// An explicit setting supplies the options and, unless it defers to the
		// default, the level.
		if setting, found := ruleSet.canonicalSetting(group, d.Name); found {
			explicit = setting
			if setting.Level() != "" && setting.Level() != "default" {
				explicitLevel = setting.Level()
			}
		}
	}
	if explicitLevel != "" {
		level = explicitLevel
	}
	if d.Experimental() && p.Experimental != nil && !*p.Experimental {
		level = "off"
	}
	if ruleSet.disabled(rules.Disable, d.Name) || cfg != nil && cfg.Lint.Enabled != nil && !*cfg.Lint.Enabled {
		level = "off"
	}
	if level == "on" {
		level = defaultLevel(d)
	}
	return explicit.WithLevel(level)
}

func (ruleSet RuleSet) DiagnosticLevel(cfg *Config, name string) Level {
	name = ruleSet.ResolveAlias(name)
	if cfg != nil {
		if cfg.Lint.Enabled != nil && !*cfg.Lint.Enabled || ruleSet.disabled(cfg.Lint.Rules.Disable, name) {
			return "off"
		}
		if level, ok := cfg.Lint.Rules.ResolvedLevel(name); ok {
			return level
		}
	}
	if d, ok := ruleSet.FindRule(name); ok {
		return ruleSet.SettingFor(cfg, d, Presets{}, false).Level()
	}
	return "on"
}

// ResolveRules projects presets and explicit rule settings into an effective configuration.
// Registered descriptors participate in selection without loading analyzer implementations.
func (ruleSet RuleSet) ResolveRules(p Presets, config *Config, descriptors ...rule.Descriptor) (*Config, error) {
	registrations := make([]RuleRegistration, len(descriptors))
	for i, descriptor := range descriptors {
		registrations[i] = RuleRegistration{Descriptor: descriptor}
	}
	return ruleSet.ResolveRegisteredRules(p, config, registrations...)
}

// RuleRegistration identifies a rule and, for a selected tool integration, the
// bundled tool whose descriptor it replaces. Ordinary analyzers leave ReplacesTool empty.
type RuleRegistration struct {
	Descriptor      rule.Descriptor
	Scope           rule.ConfigurationScope
	SupportsActions bool
	ReplacesTool    string
	// Options overrides repository options when the registration supplies JSON.
	Options json.RawMessage
}

// ResolveRegisteredRules binds descriptors with their validated tool roles.
func (ruleSet RuleSet) ResolveRegisteredRules(p Presets, config *Config, registrations ...RuleRegistration) (*Config, error) {
	if config == nil && !p.Strict && p.Experimental == nil && len(registrations) == 0 {
		return nil, nil
	}
	registered, err := ruleSet.prepareRegistrations(registrations)
	if err != nil {
		return nil, err
	}
	ruleSet = registered.rules
	config, err = ruleSet.capturedRuleConfig(config)
	if err != nil {
		return nil, err
	}
	next := Config{}
	if config != nil {
		next = *config
	}
	resolved := make(map[string]Level)
	next.Lint.Rules.SetResolved(resolved)
	next.Lint.Rules.boundSettings = make(map[string]Setting)
	next.Lint.Rules.ruleSet = ruleSet
	descriptors := ruleSet.Rules()
	seen, optionOverrides := registered.seen, registered.options
	if err := ruleSet.bindSettings(&next, descriptors, seen, optionOverrides); err != nil {
		return nil, err
	}
	for _, d := range descriptors {
		setting := ruleSet.SettingFor(config, d, p, seen[d.Name])
		if options, overridden := optionOverrides[d.Name]; overridden {
			if err := validateOptionsJSON(d, options); err != nil {
				return nil, fmt.Errorf("options for rule %q: %w", d.Name, err)
			}
		}
		resolved[d.Name] = setting.Level()
		next.Lint.Rules.boundSettings[d.Name] = setting
		if d.Configure != nil {
			d.Configure(&next, setting)
		}
	}
	return &next, nil
}

type registeredRules struct {
	rules   RuleSet
	seen    map[string]bool
	options map[string]json.RawMessage
}

// WithRegisteredRules adds plugin declarations before selecting source-specific settings.
// It validates declarations without decoding options from an unrelated source.
func (ruleSet RuleSet) WithRegisteredRules(registrations ...RuleRegistration) (RuleSet, error) {
	registered, err := ruleSet.prepareRegistrations(registrations)
	if err != nil {
		return RuleSet{}, err
	}
	return registered.rules, nil
}

func (ruleSet RuleSet) prepareRegistrations(registrations []RuleRegistration) (*registeredRules, error) {
	descriptors := ruleSet.Rules()
	seen := map[string]bool{}
	optionOverrides := map[string]json.RawMessage{}
	for i := range descriptors {
		descriptors[i].Group = descriptors[i].Category
	}
	for _, registration := range registrations {
		descriptor := registration.Descriptor
		switch registration.Scope {
		case rule.CallerConfiguration, rule.SourceConfiguration, rule.CompositeConfiguration:
		default:
			return nil, fmt.Errorf("rule %q declares invalid configuration scope %d", descriptor.Name, registration.Scope)
		}
		if descriptor.Name == "" || strings.TrimSpace(descriptor.Name) != descriptor.Name || strings.ContainsAny(descriptor.Name, " \t\r\n") {
			return nil, fmt.Errorf("invalid rule name %q", descriptor.Name)
		}
		if canonical := ruleSet.ResolveAlias(descriptor.Name); canonical != descriptor.Name {
			return nil, fmt.Errorf("registered rule %q conflicts with existing alias for %q", descriptor.Name, canonical)
		}
		if seen[descriptor.Name] {
			return nil, fmt.Errorf("duplicate registered rule %q", descriptor.Name)
		}
		seen[descriptor.Name] = true
		if len(registration.Options) != 0 {
			optionOverrides[descriptor.Name] = slices.Clone(registration.Options)
		}
		if !rule.ValidCategory(descriptor.Category) {
			return nil, fmt.Errorf("rule %q has invalid category %q; expected one of %s", descriptor.Name, descriptor.Category, strings.Join(rule.Categories(), ", "))
		}
		if !rule.ValidMaturity(descriptor.Maturity) {
			return nil, fmt.Errorf("rule %q has invalid maturity %q", descriptor.Name, descriptor.Maturity)
		}
		if len(descriptor.OptionsSchema) > 0 {
			if _, err := configschema.Compile(descriptor.OptionsSchema); err != nil {
				return nil, fmt.Errorf("options schema for rule %q: %w", descriptor.Name, err)
			}
		}
		replaced := false
		for i := range descriptors {
			if descriptors[i].Name == descriptor.Name {
				_, bundled := toolProviders()[descriptor.Name]
				if !bundled || registration.ReplacesTool == "" || registration.ReplacesTool != descriptor.Name {
					return nil, fmt.Errorf("registered rule %q conflicts with existing rule from provider %q", descriptor.Name, descriptors[i].Provider)
				}
				descriptors[i].Descriptor = descriptor
				descriptors[i].Group = descriptor.Category
				descriptors[i].Scope = registration.Scope
				descriptors[i].SupportsActions = registration.SupportsActions
				replaced = true
				break
			}
		}
		if replaced {
			continue
		}
		descriptors = append(descriptors, Rule{Descriptor: descriptor, Group: descriptor.Category, Scope: registration.Scope, SupportsActions: registration.SupportsActions, Options: func() any { return new(map[string]any) }})
	}
	metadata := make([]rule.Descriptor, len(descriptors))
	for i, descriptor := range descriptors {
		metadata[i] = descriptor.Descriptor
	}
	names, err := rule.NewNames(metadata)
	if err != nil {
		return nil, err
	}
	return &registeredRules{rules: RuleSet{entries: descriptors, names: names, registered: seen, optionOverrides: optionOverrides}, seen: seen, options: optionOverrides}, nil
}

func (ruleSet RuleSet) bindSettings(config *Config, descriptors []Rule, registered map[string]bool, optionOverrides map[string]json.RawMessage) error {
	for _, source := range config.Lint.Rules.bindingSources {
		settings := Config{Lint: LintConfig{Rules: source.rules}}
		if err := ruleSet.bindSettings(&settings, descriptors, registered, optionOverrides); err != nil {
			return fmt.Errorf("config source %q: %w", source.origin, err)
		}
	}
	index := make(map[string]Rule, len(descriptors))
	categories := make(map[string]string, len(descriptors))
	for _, descriptor := range descriptors {
		index[descriptor.Name] = descriptor
		categories[descriptor.Name] = descriptor.Category
	}
	names := make([]string, 0, len(registered))
	for name := range registered {
		names = append(names, name)
	}
	if err := ruleSet.ValidateRuleNames(config, names...); err != nil {
		return err
	}
	config.Lint.Rules.boundCategories = categories
	lints := []*LintConfig{&config.Lint}
	for _, override := range config.Overrides {
		if override.Lint != nil {
			lints = append(lints, override.Lint)
		}
	}
	for _, lint := range lints {
		if lint.Rules.Preset != "" && !lint.Rules.Preset.Valid() {
			return fmt.Errorf("invalid rule preset %q", lint.Rules.Preset)
		}
		groups := lint.Rules.Groups()
		maps.Copy(groups, lint.Rules.LegacyGroups())
		for category, group := range groups {
			if group.Level() != "" && !group.Level().Valid() {
				return fmt.Errorf("invalid level %q in lint.rules.%s", group.Level(), category)
			}
			if group.Preset() != "" && !group.Preset().Valid() {
				return fmt.Errorf("invalid preset %q in lint.rules.%s", group.Preset(), category)
			}
			names := map[string]string{}
			for spelling, setting := range group.Rules() {
				name := ruleSet.ResolveAlias(spelling)
				if previous, duplicate := names[name]; duplicate {
					return fmt.Errorf("duplicate rule aliases %q and %q in lint.rules.%s", previous, spelling, category)
				}
				names[name] = spelling
				descriptor, known := index[name]
				if !known {
					return fmt.Errorf("unknown rule %q in lint.rules.%s", name, category)
				}
				valid := category == descriptor.Category || category == ruleSet.LegacyGroup(name)
				switch category {
				case "plugins":
					valid = registered[name]
				case "external":
					_, valid = toolProviders()[name]
				case "nursery":
					valid = descriptor.Experimental()
				}
				if !valid {
					return fmt.Errorf("rule %q belongs in lint.rules.%s, not lint.rules.%s", name, descriptor.Category, category)
				}
				if setting.Level() != "" && !setting.Level().Valid() {
					return fmt.Errorf("invalid rule level %q for %q", setting.Level(), name)
				}
				if _, overridden := optionOverrides[name]; overridden {
					continue
				}
				_, bundled := toolProviders()[name]
				if registered[name] || bundled && len(descriptor.OptionsSchema) > 0 {
					if err := validateRegisteredOptions(descriptor, setting); err != nil {
						return fmt.Errorf("options for rule %q: %w", name, err)
					}
				} else if err := validateOptions(descriptor, setting); err != nil {
					return fmt.Errorf("options for rule %q: %w", name, err)
				}
			}
		}
	}
	return nil
}

func (ruleSet RuleSet) disabled(names []string, name string) bool {
	return slices.ContainsFunc(names, func(disabled string) bool { return ruleSet.ResolveAlias(disabled) == name })
}

func validateRegisteredOptions(descriptor Rule, setting Setting) error {
	if !setting.HasOptions() {
		return nil
	}
	var options map[string]any
	if _, err := setting.LoadOptions(&options); err != nil {
		return err
	}
	document, err := json.Marshal(options)
	if err != nil {
		return err
	}
	return validateOptionsJSON(descriptor, document)
}

func validateOptionsJSON(descriptor Rule, document json.RawMessage) error {
	if !json.Valid(document) {
		return errors.New("invalid options JSON")
	}
	if len(descriptor.OptionsSchema) == 0 {
		return nil
	}
	schema, err := configschema.Compile(descriptor.OptionsSchema)
	if err != nil {
		return err
	}
	return schema.Validate(document)
}

func policyEnabled(cfg *Config, name string) (bool, bool) {
	switch name {
	case "forbidden-suppression":
		return cfg != nil && cfg.Policy.DisallowSuppressions.Enabled(), true
	case "unpinned-reference":
		return cfg.RequiresCommitHash(), true
	case "job-timeout":
		return cfg.RequiresJobTimeout().Enabled(), true
	case "explicit-permissions":
		return cfg.RequiresPermissions().Enabled(), true
	case "cache-write-untrusted", "cache-call-unrestricted", "cache-operation":
		return cfg.CachePolicyEnabled(name), true
	default:
		return false, false
	}
}

func (ruleSet RuleSet) ForDocument(config *Config, path, root string) (*Config, error) {
	if filepath.Base(path) == "action.yml" || filepath.Base(path) == "action.yaml" {
		return ruleSet.ForAction(config, path, root)
	}
	return ruleSet.ForFile(config, path, root)
}
