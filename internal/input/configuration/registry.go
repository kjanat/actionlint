package configuration

import (
	"encoding/json"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/rule"
)

// RuleSet contains the declarations available to one configuration loader.
// Construction copies metadata; later operations do not change the set.
// Its zero value declares no rules.
type RuleSet struct {
	entries         []rule.Declaration
	names           rule.Names
	registered      map[string]bool
	optionOverrides map[string]json.RawMessage
	providers       map[string]toolProvider
}

// Names returns the read-only index of current names and declared aliases.
func (ruleSet RuleSet) Names() rule.Names { return ruleSet.names }

// RuleSet returns the declarations bound to these settings.
// Nil settings have no bound declarations.
func (cfg *Config) RuleSet() RuleSet {
	if cfg == nil {
		return RuleSet{}
	}
	return cfg.Lint.Rules.ruleSet
}

// NewRuleSet validates names and indexes declarations for configuration.
// Callers supply built-in and plugin declarations; this package loads no implementations.
func NewRuleSet(declarations []rule.Declaration) (RuleSet, error) {
	descriptors := make([]rule.Descriptor, len(declarations))
	for i, declaration := range declarations {
		descriptors[i] = declaration.Descriptor.Clone()
		if !rule.ValidCategory(declaration.Category) {
			return RuleSet{}, fmt.Errorf("rule %q declares unknown category %q", declaration.Name, declaration.Category)
		}
		if declaration.DefaultLevel != "" && !Level(declaration.DefaultLevel).Valid() {
			return RuleSet{}, fmt.Errorf("rule %q declares invalid default level %q", declaration.Name, declaration.DefaultLevel)
		}
		switch declaration.Scope {
		case rule.CallerConfiguration, rule.SourceConfiguration, rule.CompositeConfiguration:
		default:
			return RuleSet{}, fmt.Errorf("rule %q declares invalid configuration scope %d", declaration.Name, declaration.Scope)
		}
		if _, legacy := new(LintRulesConfig).LegacyGroups()[declaration.LegacyGroup]; declaration.LegacyGroup != "" && !legacy && !rule.ValidCategory(declaration.LegacyGroup) {
			return RuleSet{}, fmt.Errorf("rule %q declares unknown legacy group %q", declaration.Name, declaration.LegacyGroup)
		}
		if _, known := policySettings[declaration.Policy]; declaration.Policy != "" && !known {
			return RuleSet{}, fmt.Errorf("rule %q declares unknown policy %q", declaration.Name, declaration.Policy)
		}
	}
	names, err := rule.NewNames(descriptors)
	if err != nil {
		return RuleSet{}, err
	}
	result := RuleSet{names: names, entries: make([]rule.Declaration, len(declarations))}
	for i, declaration := range declarations {
		result.entries[i] = declaration.Clone()
	}
	return result, nil
}

// WithDefaults returns metadata annotated with the host's default rule selection.
func (ruleSet RuleSet) WithDefaults(descriptor rule.Descriptor) rule.Descriptor {
	entry, ok := ruleSet.FindRule(descriptor.Name)
	if !ok {
		return descriptor
	}
	descriptor.DefaultLevel = entry.DefaultLevel
	enabled := ruleSet.SettingFor(nil, entry, Presets{}, ruleSet.registered[entry.Name]).Level() != "off"
	descriptor.DefaultEnabled = &enabled
	return descriptor
}

// Rules returns declarations with caller-owned metadata slices.
func (ruleSet RuleSet) Rules() []rule.Declaration {
	result := make([]rule.Declaration, len(ruleSet.entries))
	for i, entry := range ruleSet.entries {
		result[i] = entry.Clone()
	}
	return result
}

// RuleInfos returns metadata for schema construction and rule-name completion.
func (ruleSet RuleSet) RuleInfos() []rule.Descriptor {
	descriptors := make([]rule.Descriptor, len(ruleSet.entries))
	for i, entry := range ruleSet.entries {
		descriptors[i] = entry.Descriptor.Clone()
	}
	return descriptors
}

// ResolveAlias returns a declared rule's current name; unknown names pass through.
func (ruleSet RuleSet) ResolveAlias(name string) string { return ruleSet.names.ResolveAlias(name) }

// FindRule looks up a current name or alias and returns detached metadata.
func (ruleSet RuleSet) FindRule(name string) (rule.Declaration, bool) {
	name = ruleSet.ResolveAlias(name)
	for _, entry := range ruleSet.entries {
		if entry.Name == name {
			return entry.Clone(), true
		}
	}
	return rule.Declaration{}, false
}

// Suppressible reports whether a selector names a declared rule or an additional plugin rule.
func (ruleSet RuleSet) Suppressible(name string, additional ...string) bool {
	_, found := ruleSet.FindRule(name)
	return found || slices.Contains(additional, name)
}
