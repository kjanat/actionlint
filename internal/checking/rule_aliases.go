package checking

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/rule"
)

// ResolveRuleAlias looks up historical spellings declared by the built-in rules.
// Unknown names, including plugin IDs, pass through unchanged.
func ResolveRuleAlias(name string) string { return RuleNames().ResolveAlias(name) }

// RuleNames returns names and aliases of built-in rules and bundled tools.
func RuleNames() rule.Names { return ConfigurationRules().Names() }

// BuiltinRuleNames returns the names and aliases reserved for actionlint's native checks.
func BuiltinRuleNames() rule.Names { return RuleNames().Restrict(isNativeRule) }

// isNativeRule reports whether name is a built-in rule that actionlint itself provides.
func isNativeRule(name string) bool {
	return slices.ContainsFunc(builtins, func(declaration rule.Declaration) bool {
		return declaration.Name == name && declaration.Provider == "actionlint"
	})
}
