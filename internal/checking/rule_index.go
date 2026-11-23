package checking

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/executor/registration"

	"actionlint.kjanat.dev/v2/internal/model/rule"
)

var builtins = registration.Declarations()

func ListRules() []rule.Descriptor { return ConfigurationRules().RuleInfos() }

func FindRule(name string) (rule.Descriptor, bool) {
	declaration, found := ConfigurationRules().FindRule(name)
	return declaration.Descriptor, found
}

func IsProviderRule(name string) bool {
	declaration, found := ConfigurationRules().FindRule(name)
	return found && declaration.Name == name && !slices.ContainsFunc(builtins, func(builtin rule.Declaration) bool { return builtin.Name == name })
}

func RuleCategory(name string) string {
	descriptor, ok := FindRule(name)
	if !ok {
		return ""
	}
	return descriptor.Category
}
