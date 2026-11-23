package checking

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/rule"
)

// ResolveRuleAlias passes unknown names, including plugin IDs, through unchanged.
func ResolveRuleAlias(name string) string { return RuleNames().ResolveAlias(name) }

func RuleNames() rule.Names { return ConfigurationRules().Names() }

func BuiltinRuleNames() rule.Names { return RuleNames().Restrict(isNativeRule) }

func isNativeRule(name string) bool {
	return slices.ContainsFunc(builtins, func(declaration rule.Declaration) bool {
		return declaration.Name == name && declaration.Provider == "actionlint"
	})
}
