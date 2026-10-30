package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
	"actionlint.kjanat.dev/v2/internal/tools/provider"

	"github.com/invopop/jsonschema"
)

// toolsSchema describes tools.<name> for every bundled tool provider from its
// manifest: the host-owned keys, the provider's configuration schema and the
// shorthands folded into it. Bundled manifests are build inputs, so a defect
// panics. Providers installed by plugins are not known when generating, so
// any other name gets the host-owned shape only.
func toolsSchema(reflectMapping func(any) *jsonschema.Schema) *jsonschema.Schema {
	manifests, err := provider.Bundled()
	if err != nil {
		panic(err)
	}
	tools := &jsonschema.Schema{Type: "object", Properties: jsonschema.NewProperties(), AdditionalProperties: unknownToolSchema()}
	for _, manifest := range manifests {
		tool, err := toolSchema(manifest, reflectMapping)
		if err != nil {
			panic(fmt.Errorf("tool provider %s: %w", manifest.Rule.Name, err))
		}
		tools.Properties.Set(manifest.Rule.Name, tool)
	}
	return tools
}

func toolSchema(manifest provider.Manifest, reflectMapping func(any) *jsonschema.Schema) (*jsonschema.Schema, error) {
	mapping := reflectMapping(struct {
		Backend *toolconfig.Backend `yaml:"backend,omitempty" jsonschema:"description=Select the execution backend. Process settings apply only to the native adapter."`
		Enabled *bool               `yaml:"enabled" jsonschema:"nullable,default=true,description=Enable this tool. Omission or null keeps it enabled."`
	}{})
	mapping.Version, mapping.ID = "", ""
	mapping.Description = manifest.Rule.Description + "."
	mapping.Type = ""
	mapping.OneOf = []*jsonschema.Schema{{Type: "boolean"}, {Type: "object"}, {Type: "null"}}
	requireOneBackend(mapping)
	var document map[string]any
	if len(manifest.ConfigSchema) != 0 {
		if err := json.Unmarshal(manifest.ConfigSchema, &document); err != nil {
			return nil, err
		}
	}
	// Shorthand schemas are read before settingsSchema strips the document.
	for _, key := range slices.Sorted(maps.Keys(manifest.ConfigShorthands)) {
		property, err := propertySchema(document, manifest.ConfigShorthands[key].Path)
		if err != nil {
			return nil, fmt.Errorf("shorthand %s: %w", key, err)
		}
		mapping.Properties.Set(key, &jsonschema.Schema{Extras: property})
	}
	settings, err := settingsSchema(manifest.Rule.Name, document)
	if err != nil {
		return nil, err
	}
	mapping.Properties.Set("config", configSchema(manifest, settings))
	return mapping, nil
}

// propertySchema returns the schema of the property at path in a
// configuration schema document, one property name per nesting level.
func propertySchema(document map[string]any, path []string) (map[string]any, error) {
	schema := document
	for i, name := range path {
		properties, _ := schema["properties"].(map[string]any)
		property, ok := properties[name].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("config_schema declares no property %q", strings.Join(path[:i+1], "."))
		}
		schema = property
	}
	return schema, nil
}

// unknownToolSchema describes tools.<name> for a tool provider a plugin
// registers: only the host-owned shape is known when generating, and the
// provider validates its own settings when it loads.
func unknownToolSchema() *jsonschema.Schema {
	properties := jsonschema.NewProperties()
	properties.Set("enabled", &jsonschema.Schema{OneOf: []*jsonschema.Schema{{Type: "boolean"}, {Type: "null"}}, Description: "Enable this tool. Omission or null keeps it enabled."})
	properties.Set("backend", &jsonschema.Schema{OneOf: []*jsonschema.Schema{{Type: "object"}, {Type: "null"}}, Description: "Select the execution backend."})
	properties.Set("config", &jsonschema.Schema{OneOf: []*jsonschema.Schema{{Type: "string", MinLength: new(uint64(1))}, {Type: "object"}, {Type: "null"}}, Description: "The tool's own settings, or a configuration file path for tools that read one."})
	return &jsonschema.Schema{
		Description: "A tool provider registered by a plugin, named by its rule. Unknown names are rejected when checking.",
		OneOf: []*jsonschema.Schema{
			{Type: "boolean"},
			{Type: "null"},
			{Type: "object", AdditionalProperties: jsonschema.FalseSchema, Properties: properties},
		},
	}
}

// settingsSchema returns the schema of inline tools.<name>.config settings
// from the provider's configuration schema document. A schema published under
// schemas/<name>/ is referenced, so editors resolve it beside the root
// schema; any other is embedded. Without a configuration schema any mapping
// is accepted.
func settingsSchema(name string, document map[string]any) (*jsonschema.Schema, error) {
	if document == nil {
		return &jsonschema.Schema{Type: "object"}, nil
	}
	published, err := publishedSchema(name, document)
	if err != nil {
		return nil, err
	}
	if published != "" {
		return &jsonschema.Schema{Ref: published}, nil
	}
	// Embedded below the root, the schema is no longer its own resource.
	for _, key := range []string{"$schema", "$id", "$comment"} {
		delete(document, key)
	}
	return &jsonschema.Schema{Extras: document}, nil
}

// publishedSchema returns the slash-separated path of the versioned snapshot
// under schemas/<name>/ equal to document, or "" when none matches.
func publishedSchema(name string, document map[string]any) (string, error) {
	matches, err := filepath.Glob(filepath.Join("schemas", name, "*.schema.json"))
	if err != nil {
		return "", err
	}
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			return "", err
		}
		var snapshot map[string]any
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return "", fmt.Errorf("%s: %w", match, err)
		}
		if reflect.DeepEqual(snapshot, document) {
			return filepath.ToSlash(match), nil
		}
	}
	return "", nil
}

// configSchema describes tools.<name>.config: inline settings, and a path
// when the provider reads configuration files.
func configSchema(manifest provider.Manifest, settings *jsonschema.Schema) *jsonschema.Schema {
	markdown := "Inline settings validated by the tool's configuration schema. Omission or null uses the tool's defaults."
	alternatives := []*jsonschema.Schema{
		{Type: "object", Extras: map[string]any{"defaultSnippets": []any{map[string]string{"label": "Inline settings", "bodyText": "{}"}}}},
		{Type: "null"},
	}
	var path *jsonschema.Schema
	if manifest.ReadsConfigFiles() {
		process := manifest.Process
		markdown = "Inline settings or a configuration file or directory.\n\nRelative paths use the configuration file's directory, or the analysis working directory when supplied by an overlay. `${{ configdir }}` explicitly selects the configuration directory; `${{ gitdir }}` selects the repository root.\n\n`${{ github.workspace }}` and `${{ github.action_path }}` resolve when their context is known."
		path = &jsonschema.Schema{Type: "string", Extras: map[string]any{"defaultSnippets": []any{map[string]string{
			"label": "Configuration file or directory", "body": "${1:" + process.ConfigFiles.Names[0] + "}",
		}}}}
		alternatives = append([]*jsonschema.Schema{path}, alternatives...)
	}
	description := strings.ReplaceAll(markdown, "`", "")
	schema := &jsonschema.Schema{
		Description: description,
		Extras:      map[string]any{"markdownDescription": markdown},
		AnyOf:       alternatives,
		If:          &jsonschema.Schema{Type: "object"},
		Then: &jsonschema.Schema{AllOf: []*jsonschema.Schema{
			// Keep the selecting property's hover ahead of the referenced title.
			{Title: "config", Description: description, Extras: map[string]any{"markdownDescription": markdown}},
			settings,
		}},
	}
	if path != nil {
		// Type-only alternatives keep unknown-key diagnostics attached to the mapping.
		schema.MinLength = new(uint64(1))
		schema.Not = &jsonschema.Schema{Type: "string", Pattern: `[\x00\r\n]`}
	}
	return schema
}
