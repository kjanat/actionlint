package checking

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"actionlint.kjanat.dev/v2/internal/checking/executor/registration"

	"actionlint.kjanat.dev/v2/internal/model/bundledproviders"
	rule "actionlint.kjanat.dev/v2/internal/model/rules"
)

var builtins = registration.Declarations()

// providerRules reads rule descriptors from the bundled provider manifests.
// Invalid embedded metadata causes a panic. This package reads only the rule
// field to avoid importing the provider model and its analyzer dependency.
// TestProviderRulesComeFromManifests compares the result with provider.Bundled.
var providerRules = sync.OnceValue(func() []rule.Descriptor {
	names := bundledproviders.Names()
	rules := make([]rule.Descriptor, 0, len(names))
	for _, name := range names {
		data, _ := bundledproviders.Manifest(name)
		var manifest struct {
			Rule rule.Descriptor `json:"rule"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil || manifest.Rule.Name != name {
			panic(fmt.Sprintf("bundled tool provider %s: invalid rule: %v", name, err))
		}
		rules = append(rules, manifest.Rule)
	}
	return rules
})

// BuiltinRuleDeclarations returns the built-in rules and their configuration requirements.
// Each descriptor's slices belong to the caller.
func BuiltinRuleDeclarations() []rule.Declaration {
	declarations := slices.Clone(builtins)
	for i := range declarations {
		declarations[i].Descriptor = declarations[i].Clone()
	}
	return declarations
}

// ListRules lists the built-in checks followed by the bundled tool providers' rules.
func ListRules() []rule.Descriptor {
	rules := make([]rule.Descriptor, 0, len(builtins)+len(providerRules()))
	for _, declaration := range builtins {
		rules = append(rules, declaration.Descriptor)
	}
	rules = append(rules, providerRules()...)
	for i := range rules {
		rules[i] = rules[i].Clone()
	}
	return rules
}

// FindRule returns the built-in or bundled provider rule named name.
func FindRule(name string) (rule.Descriptor, bool) {
	name = ResolveRuleAlias(name)
	for _, r := range builtins {
		if r.Name == name {
			return r.Clone(), true
		}
	}
	for _, r := range providerRules() {
		if r.Name == name {
			return r.Clone(), true
		}
	}
	return rule.Descriptor{}, false
}

// IsProviderRule reports whether name is the rule of a bundled tool provider.
func IsProviderRule(name string) bool {
	return slices.ContainsFunc(providerRules(), func(r rule.Descriptor) bool { return r.Name == name })
}

// RuleCategory returns the semantic category used by configuration and reference
// material, independently of provider identity or experimental maturity.
func RuleCategory(name string) string {
	descriptor, ok := FindRule(name)
	if !ok {
		return ""
	}
	return descriptor.Category
}
