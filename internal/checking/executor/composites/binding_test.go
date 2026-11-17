package composites

import (
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/rulebinding"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
)

func testBindings(settings *configuration.Config) *rulebinding.Scope {
	return rulebinding.NewRegistry(settings.RuleSet()).Prepare(settings, settings, settings)
}

// testSourceBindings supplies the prepared scopes normally owned by checking.
func testSourceBindings(load func(string) (*configuration.Config, error)) func(*rulebinding.Scope, string) (*rulebinding.Scope, error) {
	prepared := make(map[string]*rulebinding.Scope)
	return func(parent *rulebinding.Scope, path string) (*rulebinding.Scope, error) {
		if sourcetext.RemoteAddress(path) {
			return parent, nil
		}
		if scope, exists := prepared[path]; exists {
			return scope, nil
		}
		settings, err := load(path)
		if err != nil {
			return nil, err
		}
		scope := rulebinding.NewRegistry(settings.RuleSet()).Prepare(parent.Caller(), settings, settings)
		prepared[path] = scope
		return scope, nil
	}
}
