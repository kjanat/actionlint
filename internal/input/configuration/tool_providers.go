package configuration

import (
	"fmt"
	"maps"
	"slices"
	"sync"

	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
	"actionlint.kjanat.dev/v2/internal/model/toolsettings"
)

// toolProvider is what configuration needs from a bundled tool provider.
type toolProvider struct {
	scope rule.ConfigurationScope
	// schema validates inline tools.<name>.config settings; nil accepts any.
	schema *toolsettings.Schema
	// shorthands may be written directly under tools.<name>.
	shorthands map[string]toolsettings.Shorthand
	// configFiles permits a path in tools.<name>.config.
	configFiles bool
}

// toolProviders indexes the bundled tool providers by rule name. The
// manifests are embedded build inputs, so a defect panics, as missing rule
// metadata does.
var toolProviders = sync.OnceValue(func() map[string]toolProvider {
	bundled, err := toolmanifest.Bundled()
	if err != nil {
		panic(fmt.Sprintf("bundled tool providers: %v", err))
	}
	index := map[string]toolProvider{}
	for _, manifest := range bundled {
		tool, err := loadToolProvider(manifest)
		if err != nil {
			panic(fmt.Sprintf("bundled tool provider %s: %v", manifest.Rule.Name, err))
		}
		index[manifest.Rule.Name] = tool
	}
	return index
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

// toolProviderNames lists the bundled tool providers' rule names in order.
func toolProviderNames() []string {
	return slices.Sorted(maps.Keys(toolProviders()))
}
