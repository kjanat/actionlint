package composites

import (
	"actionlint.kjanat.dev/v2/internal/checking/binding"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/source"
)

func testBindings(settings *config.Config) *binding.Scope {
	return binding.NewRegistry(settings.RuleSet()).Prepare(settings, settings, settings)
}

// testSourceBindings supplies the prepared scopes normally owned by checking.
func testSourceBindings(load func(string) (*config.Config, error)) func(*binding.Scope, string) (*binding.Scope, error) {
	prepared := make(map[string]*binding.Scope)
	return func(parent *binding.Scope, path string) (*binding.Scope, error) {
		if source.RemoteAddress(path) {
			return parent, nil
		}
		if scope, exists := prepared[path]; exists {
			return scope, nil
		}
		settings, err := load(path)
		if err != nil {
			return nil, err
		}
		scope := binding.NewRegistry(settings.RuleSet()).Prepare(parent.Caller(), settings, settings)
		prepared[path] = scope
		return scope, nil
	}
}
