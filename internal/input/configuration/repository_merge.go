package configuration

import (
	"maps"
	"reflect"
	"slices"
	"sync"

	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	configtree "actionlint.kjanat.dev/v2/internal/input/configuration/tree"
)

var compiledMergePlan = sync.OnceValue(func() configtree.Plan {
	plan, err := configtree.CompilePlan(reflect.TypeFor[Config]())
	if err != nil {
		panic(err)
	}
	return plan
})

// MergePlan returns detached overlay rules derived from configuration fields.
func MergePlan() configtree.Plan {
	plan := maps.Clone(compiledMergePlan())
	for path, rule := range plan {
		rule.Members = slices.Clone(rule.Members)
		plan[path] = rule
	}
	return plan
}

func loadConfigTree(path string, read func(string) ([]byte, error), normalize func(string, document.Document) (document.Document, error)) (configtree.Loaded, error) {
	return configtree.LoadWithMerge(path, read, normalize, MergePlan())
}
