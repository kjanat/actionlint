package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
	"actionlint.kjanat.dev/v2/internal/tools/provider"

	"github.com/invopop/jsonschema"
)

// toolsSchema describes tools.<name> for every bundled tool provider from its
// manifest: the host-owned keys, the provider's configuration schema and the
// shorthands folded into it. Bundled manifests are build inputs, so a defect
// panics. Providers installed by plugins are not known when generating.
func toolsSchema(reflectMapping func(any) *jsonschema.Schema) *jsonschema.Schema {
	manifests, err := provider.Bundled()
	if err != nil {
		panic(err)
	}
	tools := &jsonschema.Schema{Type: "object", Properties: jsonschema.NewProperties(), AdditionalProperties: jsonschema.FalseSchema}
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
	settings, properties, err := settingsSchema(manifest)
	if err != nil {
		return nil, err
	}
	mapping.Properties.Set("config", configSchema(manifest, settings))
	for _, key := range manifest.ConfigShorthands {
		property, ok := properties[key]
		if !ok {
			return nil, fmt.Errorf("config_schema declares no shorthand property %q", key)
		}
		mapping.Properties.Set(key, &jsonschema.Schema{Extras: property})
	}
	return mapping, nil
}

// settingsSchema returns the schema of inline tools.<name>.config settings
// and its top-level property schemas. A schema published under
// schemas/<name>/ is referenced, so editors resolve it beside the root
// schema; any other is embedded. Without a configuration schema any mapping
// is accepted.
func settingsSchema(manifest provider.Manifest) (*jsonschema.Schema, map[string]map[string]any, error) {
	if len(manifest.ConfigSchema) == 0 {
		return &jsonschema.Schema{Type: "object"}, nil, nil
	}
	var document map[string]any
	if err := json.Unmarshal(manifest.ConfigSchema, &document); err != nil {
		return nil, nil, err
	}
	properties := map[string]map[string]any{}
	if declared, ok := document["properties"].(map[string]any); ok {
		for key, value := range declared {
			if property, ok := value.(map[string]any); ok {
				properties[key] = property
			}
		}
	}
	published, err := publishedSchema(manifest.Rule.Name, document)
	if err != nil {
		return nil, nil, err
	}
	if published != "" {
		return &jsonschema.Schema{Ref: published}, properties, nil
	}
	// Embedded below the root, the schema is no longer its own resource.
	for _, key := range []string{"$schema", "$id", "$comment"} {
		delete(document, key)
	}
	return &jsonschema.Schema{Extras: document}, properties, nil
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
	if process := manifest.Process; process != nil && process.ConfigFiles != nil && len(process.ConfigFiles.Names) > 0 {
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
