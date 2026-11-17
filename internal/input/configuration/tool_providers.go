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

// toolProviders indexes the bundled tool providers by rule name. A provider
// whose embedded manifest is invalid is left out of the index, and
// BundledToolProvidersError reports why.
func toolProviders() map[string]toolProvider {
	index, _ := bundledToolProviders()
	return index
}

// BundledToolProvidersError reports why a bundled tool provider could not be
// indexed, or nil when every one was. Invalid embedded manifests are a build
// defect that TestBundledToolProvidersLoad rejects; the rule catalog reports
// this error to every check.
func BundledToolProvidersError() error {
	_, err := bundledToolProviders()
	return err
}

// bundledToolProviders decodes the bundled tool providers once per process.
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

// toolProviderNames lists the bundled tool providers' rule names in order.
func toolProviderNames() []string {
	return slices.Sorted(maps.Keys(toolProviders()))
}
