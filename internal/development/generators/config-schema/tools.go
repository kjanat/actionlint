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

	"actionlint.kjanat.dev/v2/internal/input/configuration/tools"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"

	"github.com/invopop/jsonschema"
)

func toolsSchema(reflectMapping func(any) *jsonschema.Schema) *jsonschema.Schema {
	manifests, err := toolmanifest.Bundled()
	if err != nil {
		panic(err)
	}
	toolsObject := &jsonschema.Schema{Type: "object", Properties: jsonschema.NewProperties(), AdditionalProperties: unknownToolSchema(), Extras: map[string]any{"x-tombi-additional-key-label": "tool_name"}}
	for _, manifest := range manifests {
		tool, err := toolSchema(manifest, reflectMapping)
		if err != nil {
			panic(fmt.Errorf("tool provider %s: %w", manifest.Rule.Name, err))
		}
		toolsObject.Properties.Set(manifest.Rule.Name, tool)
	}
	return toolsObject
}

func toolSchema(manifest toolmanifest.Manifest, reflectMapping func(any) *jsonschema.Schema) (*jsonschema.Schema, error) {
	mapping := reflectMapping(struct {
		Backend *tools.BackendSelection `yaml:"backend,omitempty" jsonschema:"description=Select the execution backend. Process settings apply only to the native adapter."`
		Enabled *bool                   `yaml:"enabled" jsonschema:"nullable,default=true,description=Enable this tool. Omission or null keeps it enabled."`
	}{})
	mapping.Version, mapping.ID = "", ""
	mapping.Description = manifest.Rule.Description + "."
	mapping.Type = ""
	mapping.OneOf = []*jsonschema.Schema{{Type: "boolean"}, {Type: "object"}, {Type: "null"}}
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

func unknownToolSchema() *jsonschema.Schema {
	properties := jsonschema.NewProperties()
	properties.Set("enabled", &jsonschema.Schema{OneOf: []*jsonschema.Schema{{Type: "boolean"}, {Type: "null"}}, Description: "Enable this tool. Omission or null keeps it enabled."})
	properties.Set("backend", &jsonschema.Schema{OneOf: []*jsonschema.Schema{definitionRef("backend"), {Type: "null"}}, Description: "Select the execution backend."})
	properties.Set("config", &jsonschema.Schema{OneOf: []*jsonschema.Schema{definitionRef("single-line-path"), {Type: "object", AdditionalProperties: jsonschema.TrueSchema}, {Type: "null"}}, Description: "The tool's own settings, or a configuration file path for tools that read one."})
	return &jsonschema.Schema{
		Description: "A tool provider registered by a plugin, named by its rule. Unknown names are rejected when checking.",
		OneOf: []*jsonschema.Schema{
			{Type: "boolean"},
			{Type: "null"},
			{Type: "object", AdditionalProperties: jsonschema.FalseSchema, Properties: properties},
		},
	}
}

// Editors resolve a referenced schemas/<name>/ schema beside the root schema.
func settingsSchema(name string, document map[string]any) (*jsonschema.Schema, error) {
	if document == nil {
		return &jsonschema.Schema{Type: "object", AdditionalProperties: jsonschema.TrueSchema}, nil
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

func configSchema(manifest toolmanifest.Manifest, settings *jsonschema.Schema) *jsonschema.Schema {
	markdown := "Inline settings validated by the tool's configuration schema. Omission or null uses the tool's defaults."
	alternatives := []*jsonschema.Schema{
		{Type: "object", AdditionalProperties: jsonschema.TrueSchema, Extras: map[string]any{"defaultSnippets": []any{map[string]string{"label": "Inline settings", "bodyText": "{}"}}}},
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
		If:          &jsonschema.Schema{Type: "object", AdditionalProperties: jsonschema.TrueSchema},
		Then: &jsonschema.Schema{AllOf: []*jsonschema.Schema{
			// This entry supplies hover text before the referenced schema's title.
			{Title: "config", Description: description, AdditionalProperties: jsonschema.TrueSchema, Extras: map[string]any{"markdownDescription": markdown}},
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
