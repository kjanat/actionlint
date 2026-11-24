package configuration

import (
	"maps"
	"reflect"
	"slices"
	"sync"

	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
)

var compiledMergePlan = sync.OnceValue(func() document.Plan {
	plan, err := document.CompilePlan(reflect.TypeFor[Config]())
	if err != nil {
		panic(err)
	}
	return plan
})

func MergePlan() document.Plan {
	plan := maps.Clone(compiledMergePlan())
	for path, rule := range plan {
		rule.Members = slices.Clone(rule.Members)
		plan[path] = rule
	}
	return plan
}

// LoadSources reuses sources held by files.
func LoadSources(path string, files *document.Files, read func(string) ([]byte, error), normalize document.Normalizer) (document.Loaded, error) {
	return document.LoadConfigDocument(path, files, read, normalize, MergePlan())
}

func relocateSource(value any, source *document.Node, relocated map[string]document.Location) *document.Node {
	locations := source.Origins()
	for pointer, location := range relocated {
		if _, parsed := locations[pointer]; !parsed {
			locations[pointer] = location
		}
	}
	return document.Capture(value, locations)
}
