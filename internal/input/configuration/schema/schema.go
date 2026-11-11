// Package schema builds the configuration JSON schema that
// "actionlint config schema" prints. It starts from the generated
// actionlint.schema.json, inlines the schema files it references so the
// printed document stands alone, and adds a tools.<name> entry for each tool
// provider that plugins install. The generated file describes only the
// bundled providers, because installed ones are not known when it is
// generated.
package schema

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"path"
	"slices"
	"strings"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	pluginmanifests "actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

// RootFile is the name of the generated configuration schema that Document
// reads first.
const RootFile = "actionlint.schema.json"

// ReadFunc returns the schema file with the slash-separated name, such as
// RootFile or a file it references. actionlint.Schema is one.
type ReadFunc func(name string) ([]byte, error)

// object is a decoded JSON object.
type object = map[string]any

// Document returns the configuration schema that read returns for RootFile,
// with every relative $ref replaced by the file it names and a
// tools.<rule.name> entry for each manifest. The entry validates
// tools.<name>.config with the manifest's config_schema and
// process.config_schema, and accepts a configuration file path only when the
// manifest reads configuration files. Installed providers replace bundled
// entries with the same rule name.
func Document(read ReadFunc, manifests []pluginmanifests.Manifest) ([]byte, error) {
	root, err := readObject(read, RootFile)
	if err != nil {
		return nil, err
	}
	if err := inline(read, path.Dir(RootFile), root, nil); err != nil {
		return nil, err
	}
	mappings := toolMaps(root)
	if len(mappings) == 0 {
		return nil, fmt.Errorf("%s describes no tools mapping", RootFile)
	}
	settings, err := addToolSettings(root, manifests)
	if err != nil {
		return nil, err
	}
	for _, tools := range mappings {
		if err := addTools(tools, manifests, settings); err != nil {
			return nil, err
		}
	}
	if err := addPluginRules(root, manifests); err != nil {
		return nil, err
	}
	root["$comment"] = "Printed by actionlint config schema from " + RootFile + " and the tool providers that plugins install."
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// addPluginRules specializes the shape-valid unknown rule entries generated
// for each semantic category with every installed manifest's option schema.
func addPluginRules(root object, manifests []pluginmanifests.Manifest) error {
	categories := ruleCategoryMaps(root)
	if len(categories) == 0 {
		return errors.New("configuration schema describes no lint.rules category mappings")
	}
	definitions, _ := root["$defs"].(object)
	if definitions == nil {
		definitions = object{}
		root["$defs"] = definitions
	}
	knownNames, ok := definitions["known-rule-name"].(object)
	if !ok {
		return errors.New("configuration schema describes no known-rule-name definition")
	}
	values, ok := knownNames["anyOf"].([]any)
	if !ok {
		return errors.New("configuration schema describes no known rule names")
	}
	choices := make(map[string]object, len(values)+len(manifests))
	for _, value := range values {
		choice, ok := value.(object)
		name, named := choice["const"].(string)
		if !ok || !named {
			return errors.New("configuration schema contains a non-string rule name")
		}
		choices[name] = choice
	}
	for _, manifest := range manifests {
		for _, choice := range RuleNameChoices([]rule.Descriptor{manifest.Rule}) {
			name, _ := choice["const"].(string)
			if _, exists := choices[name]; !exists {
				choices[name] = choice
			}
		}
	}
	values = nil
	names := make([]any, 0, len(choices))
	for _, name := range slices.Sorted(maps.Keys(choices)) {
		values = append(values, choices[name])
		names = append(names, name)
	}
	knownNames["anyOf"] = values
	definitions["known-rule-name-values"] = object{"enum": names}
	seen := make(map[string]string, len(manifests))
	for _, manifest := range manifests {
		category, name := manifest.Rule.Category, manifest.Rule.Name
		if !rule.ValidCategory(category) {
			return fmt.Errorf("rule %s: unsupported category %q", name, category)
		}
		if previous, exists := seen[name]; exists {
			return fmt.Errorf("duplicate plugin rule %s in categories %s and %s", name, previous, category)
		}
		seen[name] = category
		for otherCategory, mappings := range categories {
			for _, categorySchema := range mappings {
				properties, _ := categorySchema["properties"].(object)
				if _, exists := properties[name]; exists && otherCategory != category {
					return fmt.Errorf("rule %s declares category %s but is already declared under %s", name, category, otherCategory)
				}
			}
		}
		setting := object{"allOf": []any{object{"$ref": "#/$defs/rule-setting"}}, "title": name, "description": manifest.Rule.Description, "markdownDescription": manifest.Rule.Description}
		if len(manifest.Rule.OptionsSchema) != 0 {
			options, err := decodeObject(manifest.Rule.OptionsSchema, "options_schema")
			if err != nil {
				return fmt.Errorf("rule %s: %w", name, err)
			}
			definition := "plugin-rule-options-" + base64.RawURLEncoding.EncodeToString([]byte(name))
			if _, exists := definitions[definition]; exists {
				return fmt.Errorf("multiple plugin rules map to schema definition %q", definition)
			}
			definitions[definition] = options
			if _, hasID := options["$id"]; !hasID {
				options["$id"] = "urn:actionlint:plugin-rule-options:" + base64.RawURLEncoding.EncodeToString([]byte(name))
			}
			setting["allOf"] = []any{
				object{"$ref": "#/$defs/plugin-setting"},
				object{"additionalProperties": true, "properties": object{"options": object{"anyOf": []any{object{"type": "null"}, object{"$ref": "#/$defs/" + definition}}}}},
			}
		}
		for _, categorySchema := range categories[category] {
			properties, _ := categorySchema["properties"].(object)
			if _, exists := properties[name]; exists {
				continue
			}
			properties[name] = setting
		}
		for otherCategory, mappings := range categories {
			if otherCategory == category {
				continue
			}
			for _, categorySchema := range mappings {
				propertyNames, _ := categorySchema["propertyNames"].(object)
				if propertyNames == nil {
					propertyNames = object{}
				}
				not, _ := propertyNames["not"].(object)
				names, _ := not["enum"].([]any)
				propertyNames["not"] = object{"enum": append(names, name)}
				categorySchema["propertyNames"] = propertyNames
			}
		}
	}
	return nil
}

// ruleCategoryMaps returns each category's object mapping under lint.rules,
// including the corresponding mapping under overrides.
func ruleCategoryMaps(value any) map[string][]object {
	result := make(map[string][]object)
	for _, category := range rule.Categories() {
		result[category] = nil
	}
	var walk func(any)
	walk = func(value any) {
		switch node := value.(type) {
		case object:
			if properties, ok := node["properties"].(object); ok {
				allCategories := true
				for category := range result {
					if _, exists := properties[category]; !exists {
						allCategories = false
						break
					}
				}
				if allCategories {
					for category := range result {
						if mapping := objectMapping(properties[category]); mapping != nil {
							result[category] = append(result[category], mapping)
						}
					}
				}
			}
			for _, child := range node {
				walk(child)
			}
		case []any:
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(value)
	for _, mappings := range result {
		if len(mappings) == 0 {
			return nil
		}
	}
	return result
}

func objectMapping(value any) object {
	property, ok := value.(object)
	if !ok {
		return nil
	}
	alternatives, _ := property["oneOf"].([]any)
	for _, alternative := range alternatives {
		mapping, ok := alternative.(object)
		if !ok || mapping["type"] != "object" {
			continue
		}
		if _, ok := mapping["properties"].(object); ok {
			return mapping
		}
	}
	return nil
}

// readObject decodes the JSON object in file name. Numbers keep their text.
func readObject(read ReadFunc, name string) (object, error) {
	data, err := read(name)
	if err != nil {
		return nil, err
	}
	return decodeObject(data, name)
}

// decodeObject decodes data, a JSON object read from source.
func decodeObject(data []byte, source string) (object, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value object
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	if value == nil {
		return nil, fmt.Errorf("%s: want a JSON object", source)
	}
	return value, nil
}

// inline replaces every $ref in value that names a file, relative to
// directory, with that file's schema. A node {"$ref": F, ...} becomes
// {..., "allOf": [..., <F>]}, which validates the same instances. visiting
// lists the files being inlined, to refuse a reference cycle.
func inline(read ReadFunc, directory string, value any, visiting []string) error {
	switch node := value.(type) {
	case object:
		for _, key := range slices.Sorted(maps.Keys(node)) {
			if err := inline(read, directory, node[key], visiting); err != nil {
				return err
			}
		}
		ref, ok := node["$ref"].(string)
		if !ok || strings.HasPrefix(ref, "#") || strings.Contains(ref, ":") {
			return nil
		}
		name := path.Join(directory, ref)
		if slices.Contains(visiting, name) {
			return fmt.Errorf("schema reference cycle through %s", name)
		}
		referenced, err := readObject(read, name)
		if err != nil {
			return err
		}
		if err := inline(read, path.Dir(name), referenced, append(visiting, name)); err != nil {
			return err
		}
		embedded(referenced)
		delete(node, "$ref")
		allOf, _ := node["allOf"].([]any)
		node["allOf"] = append(allOf, referenced)
	case []any:
		for _, item := range node {
			if err := inline(read, directory, item, visiting); err != nil {
				return err
			}
		}
	}
	return nil
}

// embedded removes the keys that only a schema's root may carry, so the
// schema can sit inside another document.
func embedded(schema object) {
	for _, key := range []string{"$schema", "$id", "$comment"} {
		delete(schema, key)
	}
}

// toolMaps returns the object schemas of every tools mapping in the
// configuration schema: the top-level one and the one in each override. A
// tools mapping is the object alternative of a "tools" property that lists
// its entries under properties and describes unknown names with
// additionalProperties. The search does not descend into a mapping it found.
func toolMaps(value any) []object {
	var found []object
	var walk func(any)
	walk = func(value any) {
		switch node := value.(type) {
		case object:
			if properties, ok := node["properties"].(object); ok {
				if tools, ok := toolsMapping(properties["tools"]); ok {
					found = append(found, tools)
				}
			}
			for _, key := range []string{"properties", "items", "oneOf", "anyOf", "allOf", "additionalProperties"} {
				child := node[key]
				if properties, ok := child.(object); ok && key == "properties" {
					for _, name := range slices.Sorted(maps.Keys(properties)) {
						if name != "tools" {
							walk(properties[name])
						}
					}
					continue
				}
				walk(child)
			}
		case []any:
			for _, item := range node {
				walk(item)
			}
		}
	}
	walk(value)
	return found
}

// toolsMapping returns the object alternative of a tools property schema.
func toolsMapping(value any) (object, bool) {
	property, ok := value.(object)
	if !ok {
		return nil, false
	}
	alternatives, _ := property["oneOf"].([]any)
	for _, alternative := range alternatives {
		mapping, ok := alternative.(object)
		if !ok || mapping["type"] != "object" {
			continue
		}
		_, named := mapping["properties"].(object)
		_, unknown := mapping["additionalProperties"].(object)
		if named && unknown {
			return mapping, true
		}
	}
	return nil, false
}

// addTools adds an entry for each manifest to the tools mapping, built from
// the mapping's schema for unknown names.
func addTools(tools object, manifests []pluginmanifests.Manifest, settings map[string]object) error {
	entries, _ := tools["properties"].(object)
	unknown, _ := tools["additionalProperties"].(object)
	for _, manifest := range manifests {
		name := manifest.Rule.Name
		entry, err := toolEntry(unknown, manifest, settings[name])
		if err != nil {
			return fmt.Errorf("tool provider %s: %w", name, err)
		}
		entries[name] = entry
	}
	return nil
}

// toolEntry returns the tools.<name> schema of manifest: a copy of the
// schema for unknown names whose config property validates the manifest's
// settings.
func toolEntry(unknown object, manifest pluginmanifests.Manifest, settings object) (object, error) {
	copied, err := deepCopy(unknown)
	if err != nil {
		return nil, err
	}
	description := strings.TrimSuffix(manifest.Rule.Description, ".") + "."
	copied["description"], copied["markdownDescription"] = description, description
	alternatives, _ := copied["oneOf"].([]any)
	for _, alternative := range alternatives {
		mapping, ok := alternative.(object)
		if !ok || mapping["type"] != "object" {
			continue
		}
		properties, _ := mapping["properties"].(object)
		config, ok := properties["config"].(object)
		if !ok {
			return nil, errors.New("the schema for unknown tools has no config property")
		}
		values := []any{settings, object{"type": "null"}}
		if manifest.ReadsConfigFiles() {
			values = append([]any{object{"$ref": "#/$defs/single-line-path"}}, values...)
		}
		delete(config, "oneOf")
		config["anyOf"] = values
		for _, key := range slices.Sorted(maps.Keys(manifest.ConfigShorthands)) {
			var ref strings.Builder
			ref.WriteString("#/$defs/" + toolSettingsDefinition(manifest.Rule.Name, 0))
			for _, property := range manifest.ConfigShorthands[key].Path {
				ref.WriteString("/properties/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(property))
			}
			properties[key] = object{"$ref": ref.String()}
			conditions, _ := mapping["allOf"].([]any)
			mapping["allOf"] = append(conditions, object{
				"if":   object{"required": []any{key}},
				"then": object{"properties": object{"config": object{"type": []any{"object", "null"}}}},
			})
		}
		return copied, nil
	}
	return nil, errors.New("the schema for unknown tools has no mapping alternative")
}

// addToolSettings places each provider schema in a single resource shared by
// the root tools mapping and every override.
func addToolSettings(root object, manifests []pluginmanifests.Manifest) (map[string]object, error) {
	definitions, _ := root["$defs"].(object)
	if definitions == nil {
		definitions = object{}
		root["$defs"] = definitions
	}
	result := make(map[string]object, len(manifests))
	for _, manifest := range manifests {
		settings, err := settingsSchema(definitions, manifest)
		if err != nil {
			return nil, fmt.Errorf("tool provider %s: %w", manifest.Rule.Name, err)
		}
		result[manifest.Rule.Name] = settings
	}
	return result, nil
}

// settingsSchema combines a provider's common and process settings schemas.
func settingsSchema(definitions object, manifest pluginmanifests.Manifest) (object, error) {
	documents := []json.RawMessage{manifest.ConfigSchema}
	if manifest.Process != nil {
		documents = append(documents, manifest.Process.ConfigSchema)
	}
	settings := object{"type": "object", "additionalProperties": true}
	var all []any
	for i, document := range documents {
		if len(document) == 0 {
			continue
		}
		schema, err := decodeObject(document, "config_schema")
		if err != nil {
			return nil, err
		}
		name := toolSettingsDefinition(manifest.Rule.Name, i)
		if _, exists := definitions[name]; exists {
			return nil, fmt.Errorf("duplicate configuration schema definition %q", name)
		}
		var id string
		if value, exists := schema["$id"]; exists {
			var valid bool
			id, valid = value.(string)
			if !valid {
				return nil, errors.New("configuration schema $id must be a string")
			}
		}
		parsed, err := url.Parse(id)
		if err != nil {
			return nil, fmt.Errorf("configuration schema $id: %w", err)
		}
		if !parsed.IsAbs() {
			// A distinct host also separates IDs that start with / or ../.
			digest := sha256.Sum256([]byte(name))
			base := &url.URL{Scheme: "https", Host: fmt.Sprintf("%x.%x.schema.actionlint.invalid", digest[:16], digest[16:]), Path: "/schema.json"}
			schema["$id"] = base.ResolveReference(parsed).String()
		}
		definitions[name] = schema
		all = append(all, object{"$ref": "#/$defs/" + name})
	}
	if len(all) != 0 {
		settings["allOf"] = all
	}
	return settings, nil
}

func toolSettingsDefinition(name string, index int) string {
	return fmt.Sprintf("plugin-tool-config-%s-%d", base64.RawURLEncoding.EncodeToString([]byte(name)), index)
}

// deepCopy returns a copy of value that shares no storage with it.
func deepCopy(value object) (object, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return decodeObject(data, "schema")
}
