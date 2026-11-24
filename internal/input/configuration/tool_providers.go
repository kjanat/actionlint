package configuration

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
	"actionlint.kjanat.dev/v2/internal/model/toolsettings"
)

type toolProvider struct {
	scope rule.ConfigurationScope
	// schema validates inline tools.<name>.config settings; nil accepts any.
	schema      *toolsettings.Schema
	shorthands  map[string]toolsettings.Shorthand
	configFiles bool
}

func toolProviders() map[string]toolProvider {
	index, _ := bundledToolProviders()
	return index
}

func BundledToolProvidersError() error {
	_, err := bundledToolProviders()
	return err
}

var bundledToolProviders = sync.OnceValues(func() (map[string]toolProvider, error) {
	index := map[string]toolProvider{}
	bundled, err := toolmanifest.Bundled()
	if err != nil {
		return index, fmt.Errorf("bundled tool providers: %w", err)
	}
	var failures []error
	for _, manifest := range bundled {
		tool, err := loadToolProvider(manifest)
		if err != nil {
			failures = append(failures, fmt.Errorf("bundled tool provider %s: %w", manifest.Rule.Name, err))
			continue
		}
		index[manifest.Rule.Name] = tool
	}
	return index, errors.Join(failures...)
})

func loadToolProvider(manifest toolmanifest.Manifest) (toolProvider, error) {
	tool, err := compileToolProvider(manifest.Settings())
	if err != nil {
		return toolProvider{}, err
	}
	scope, err := manifest.Scope.ConfigurationScope()
	if err != nil {
		return toolProvider{}, err
	}
	tool.scope = scope
	return tool, nil
}

func compileToolProvider(settings toolsettings.Spec) (toolProvider, error) {
	settings = settings.Clone()
	for name, shorthand := range settings.Shorthands {
		if len(shorthand.Path) == 0 {
			return toolProvider{}, fmt.Errorf("shorthand %q has no property path", name)
		}
	}
	tool := toolProvider{shorthands: settings.Shorthands, configFiles: settings.ReadsFiles}
	if len(settings.Schema) != 0 {
		schema, err := toolsettings.Compile(settings.Schema)
		if err != nil {
			return toolProvider{}, fmt.Errorf("config_schema: %w", err)
		}
		tool.schema = schema
	}
	return tool, nil
}

func (ruleSet RuleSet) toolProvider(name string) (toolProvider, bool) {
	if provider, ok := ruleSet.providers[name]; ok {
		return provider, true
	}
	provider, ok := toolProviders()[name]
	return provider, ok
}

func toolProviderNames() []string {
	return slices.Sorted(maps.Keys(toolProviders()))
}
