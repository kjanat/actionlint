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

// MergePlan returns detached overlay rules derived from configuration fields.
func MergePlan() document.Plan {
	plan := maps.Clone(compiledMergePlan())
	for path, rule := range plan {
		rule.Members = slices.Clone(rule.Members)
		plan[path] = rule
	}
	return plan
}

// LoadSources reads a configuration file and the files it extends into one
// merged tree, using the merge rules of Config. normalize rewrites each source
// before it is merged. Sources held by files are not read again.
func LoadSources(path string, files *document.Files, read func(string) ([]byte, error), normalize document.Normalizer) (document.Loaded, error) {
	return document.Load(path, files, read, normalize, MergePlan())
}

// relocateSource returns value located by source, adding the locations of
// settings that migration moved. Settings that stay in place keep their
// parsed locations.
func relocateSource(value any, source *document.Node, relocated map[string]document.Location) *document.Node {
	locations := source.Origins()
	for pointer, location := range relocated {
		if _, parsed := locations[pointer]; !parsed {
			locations[pointer] = location
		}
	}
	return document.Capture(value, locations)
}
