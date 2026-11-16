package checking

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/executor/registration"

	"actionlint.kjanat.dev/v2/internal/model/manifests"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

var builtins = registration.Declarations()

// bundledManifests returns the bundled provider manifests.
// Invalid embedded metadata causes a panic.
func bundledManifests() []manifests.Manifest {
	manifests, err := manifests.Bundled()
	if err != nil {
		panic(err)
	}
	return manifests
}

// providerRules lists the bundled providers' rules in directory order.
func providerRules() []rule.Descriptor {
	manifests := bundledManifests()
	rules := make([]rule.Descriptor, len(manifests))
	for i, manifest := range manifests {
		rules[i] = manifest.Rule
	}
	return rules
}

// BuiltinRuleDeclarations returns the built-in rules and their configuration requirements.
// Each descriptor's slices belong to the caller.
func BuiltinRuleDeclarations() []rule.Declaration {
	declarations := slices.Clone(builtins)
	for i := range declarations {
		declarations[i] = declarations[i].Clone()
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
			return r.Descriptor.Clone(), true
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
