package rulecatalog

import (
	"sync"

	"actionlint.kjanat.dev/v2/rule"
)

var builtinNames = sync.OnceValue(func() rule.Names {
	index, err := rule.NewNames(Rules())
	if err != nil {
		panic(err)
	}
	return index
})

// ResolveAlias looks up historical spellings declared by the built-in rules.
// Unknown names, including plugin IDs, pass through unchanged.
func ResolveAlias(name string) string { return Names().ResolveAlias(name) }

// Names returns the name index derived from built-in rule declarations.
func Names() rule.Names { return builtinNames() }
