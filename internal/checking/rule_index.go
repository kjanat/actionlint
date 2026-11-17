package checking

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/executor/registration"

	"actionlint.kjanat.dev/v2/internal/model/rule"
)

var builtins = registration.Declarations()

// ListRules lists the built-in checks followed by the bundled tool providers'
// rules, as the configuration rules declare them.
func ListRules() []rule.Descriptor { return ConfigurationRules().RuleInfos() }

// FindRule returns the built-in or bundled provider rule named name.
func FindRule(name string) (rule.Descriptor, bool) {
	declaration, found := ConfigurationRules().FindRule(name)
	return declaration.Descriptor, found
}

// IsProviderRule reports whether name is the rule of a bundled tool provider.
func IsProviderRule(name string) bool {
	declaration, found := ConfigurationRules().FindRule(name)
	return found && declaration.Name == name && !slices.ContainsFunc(builtins, func(builtin rule.Declaration) bool { return builtin.Name == name })
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
