package checking

import (
	"sync"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

var bundledRuleNames = sync.OnceValue(func() rule.Names {
	index, err := rule.NewNames(ListRules())
	if err != nil {
		panic(err)
	}
	return index
})

// ResolveRuleAlias looks up historical spellings declared by the built-in rules.
// Unknown names, including plugin IDs, pass through unchanged.
func ResolveRuleAlias(name string) string { return RuleNames().ResolveAlias(name) }

// RuleNames returns names and aliases of built-in rules and bundled tools.
func RuleNames() rule.Names { return bundledRuleNames() }

// BuiltinRuleNames returns the names and aliases reserved for actionlint's native checks.
func BuiltinRuleNames() rule.Names { return nativeRuleNames() }

var nativeRuleNames = sync.OnceValue(func() rule.Names {
	descriptors := make([]rule.Descriptor, 0, len(builtins))
	for _, declaration := range builtins {
		if declaration.Provider == "actionlint" {
			descriptors = append(descriptors, declaration.Descriptor)
		}
	}
	index, err := rule.NewNames(descriptors)
	if err != nil {
		panic(err)
	}
	return index
})
