package config

import (
	"encoding/json"
	"fmt"
	"slices"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// RuleSet contains the declarations available to one configuration loader.
// Construction copies metadata; later operations do not change the set.
// Its zero value declares no rules.
type RuleSet struct {
	entries         []Rule
	names           rulemeta.Names
	registered      map[string]bool
	optionOverrides map[string]json.RawMessage
}

// Names returns the read-only index of current names and declared aliases.
func (ruleSet RuleSet) Names() rulemeta.Names { return ruleSet.names }

// RuleSet returns the declarations bound to these settings.
// Nil settings have no bound declarations.
func (cfg *Config) RuleSet() RuleSet {
	if cfg == nil {
		return RuleSet{}
	}
	return cfg.Lint.Rules.ruleSet
}

// NewRuleSet validates names and projects declarations into configuration rules.
// Callers supply built-in and plugin declarations; this package loads no implementations.
func NewRuleSet(declarations []rulemeta.Declaration) (RuleSet, error) {
	descriptors := make([]rulemeta.Descriptor, len(declarations))
	for i, declaration := range declarations {
		descriptors[i] = declaration.Clone()
		if !rulemeta.ValidCategory(declaration.Category) {
			return RuleSet{}, fmt.Errorf("rule %q declares unknown category %q", declaration.Name, declaration.Category)
		}
		if declaration.DefaultLevel != "" && !Level(declaration.DefaultLevel).Valid() {
			return RuleSet{}, fmt.Errorf("rule %q declares invalid default level %q", declaration.Name, declaration.DefaultLevel)
		}
		switch declaration.Scope {
		case rulemeta.CallerConfiguration, rulemeta.SourceConfiguration, rulemeta.CompositeConfiguration:
		default:
			return RuleSet{}, fmt.Errorf("rule %q declares invalid configuration scope %d", declaration.Name, declaration.Scope)
		}
	}
	names, err := rulemeta.NewNames(descriptors)
	if err != nil {
		return RuleSet{}, err
	}
	projectors := map[string]func(*Config, Setting){
		"forbidden-suppression":   configureSuppressionsPolicy,
		"unpinned-reference":      boolRuleOption(func(p *Policy) **bool { return &p.RequireCommitHash }),
		"missing-action-metadata": boolRuleOption(func(p *Policy) **bool { return &p.RequireActionMetadata }),
		"job-timeout":             configureJobTimeout,
		"explicit-permissions":    configurePermissions,
		"cache-write-untrusted":   boolRuleOption(func(p *Policy) **bool { return &p.CacheWriteUntrusted }),
		"cache-call-unrestricted": boolRuleOption(func(p *Policy) **bool { return &p.CacheCallUnrestricted }),
		"cache-operation":         boolRuleOption(func(p *Policy) **bool { return &p.CacheOperation }),
	}
	result := RuleSet{names: names, entries: make([]Rule, len(declarations))}
	for i, declaration := range declarations {
		result.entries[i] = Rule{
			Descriptor: descriptors[i], Group: declaration.Category,
			SupportsActions: declaration.SupportsActions, Scope: declaration.Scope,
			DefaultLevel: Level(declaration.DefaultLevel), Options: declaration.Options,
			Configure: projectors[declaration.Name],
		}
	}
	return result, nil
}

// Rules returns configuration rules with caller-owned metadata slices.
func (ruleSet RuleSet) Rules() []Rule {
	result := slices.Clone(ruleSet.entries)
	for i := range result {
		result[i].Descriptor = result[i].Clone()
	}
	return result
}

// RuleInfos returns metadata for schema construction and rule-name completion.
func (ruleSet RuleSet) RuleInfos() []rulemeta.Descriptor {
	descriptors := make([]rulemeta.Descriptor, len(ruleSet.entries))
	for i, entry := range ruleSet.entries {
		descriptors[i] = entry.Clone()
	}
	return descriptors
}

// ResolveAlias returns a declared rule's current name; unknown names pass through.
func (ruleSet RuleSet) ResolveAlias(name string) string { return ruleSet.names.ResolveAlias(name) }

// FindRule looks up a current name or alias and returns detached metadata.
func (ruleSet RuleSet) FindRule(name string) (Rule, bool) {
	name = ruleSet.ResolveAlias(name)
	for _, entry := range ruleSet.entries {
		if entry.Name == name {
			entry.Descriptor = entry.Clone()
			return entry, true
		}
	}
	return Rule{}, false
}

// Suppressible reports whether a selector names a declared rule or an additional plugin rule.
func (ruleSet RuleSet) Suppressible(name string, additional ...string) bool {
	_, found := ruleSet.FindRule(name)
	return found || slices.Contains(additional, name)
}
