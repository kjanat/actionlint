package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"actionlint.kjanat.dev/v2/internal/tools/provider/bundled"
	"actionlint.kjanat.dev/v2/internal/tools/provider/configschema"
)

// toolProvider is what configuration needs from a bundled tool provider.
type toolProvider struct {
	scope Scope
	// schema validates inline tools.<name>.config settings; nil accepts any.
	schema *configschema.Schema
	// shorthands may be written directly under tools.<name>.
	shorthands map[string]configschema.Shorthand
	// configFiles permits a path in tools.<name>.config.
	configFiles bool
}

// providerManifest is the part of a provider manifest configuration reads.
// The manifest model in package provider imports the analyzer contract, which
// configuration must not depend on, so configuration reads these fields from
// the bundled data directly. TestToolProvidersMatchManifests keeps this view
// equal to provider.Bundled.
type providerManifest struct {
	Scope            string                            `json:"scope"`
	ConfigSchema     json.RawMessage                   `json:"config_schema"`
	ConfigShorthands map[string]configschema.Shorthand `json:"config_shorthands"`
	Process          *struct {
		ConfigFiles *struct {
			Names []string `json:"names"`
		} `json:"config_files"`
	} `json:"process"`
}

// toolProviders indexes the bundled tool providers by rule name. The
// manifests are embedded build inputs, so a defect panics, as missing rule
// metadata does.
var toolProviders = sync.OnceValue(func() map[string]toolProvider {
	index := map[string]toolProvider{}
	for _, name := range bundled.Names() {
		tool, err := loadToolProvider(name)
		if err != nil {
			panic(fmt.Sprintf("bundled tool provider %s: %v", name, err))
		}
		index[name] = tool
	}
	return index
})

func loadToolProvider(name string) (toolProvider, error) {
	data, ok := bundled.Manifest(name)
	if !ok {
		return toolProvider{}, errors.New("no manifest")
	}
	var manifest providerManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return toolProvider{}, err
	}
	tool := toolProvider{shorthands: manifest.ConfigShorthands}
	switch manifest.Scope {
	case "composite":
		tool.scope = CompositeScope
	case "source":
		tool.scope = SourceScope
	default:
		return toolProvider{}, fmt.Errorf("unknown scope %q", manifest.Scope)
	}
	if len(manifest.ConfigSchema) != 0 {
		schema, err := configschema.Compile(manifest.ConfigSchema)
		if err != nil {
			return toolProvider{}, fmt.Errorf("config_schema: %w", err)
		}
		tool.schema = schema
	}
	tool.configFiles = manifest.Process != nil && manifest.Process.ConfigFiles != nil && len(manifest.Process.ConfigFiles.Names) > 0
	return tool, nil
}

// toolProviderNames lists the bundled tool providers' rule names in order.
func toolProviderNames() []string {
	return slices.Sorted(maps.Keys(toolProviders()))
}
