// Package editorschema builds the configuration JSON schema that
// "actionlint config schema" prints. It starts from the generated
// actionlint.schema.json, inlines the schema files it references so the
// printed document stands alone, and adds a tools.<name> entry for each tool
// provider that plugins install. The generated file describes only the
// bundled providers, because installed ones are not known when it is
// generated.
package editorschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/tools/provider"
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
// manifest reads configuration files. A manifest whose rule name the
// generated schema already describes is skipped: configuration validates
// that name against the bundled provider's manifest.
func Document(read ReadFunc, manifests []provider.Manifest) ([]byte, error) {
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
	for _, tools := range mappings {
		if err := addTools(tools, manifests); err != nil {
			return nil, err
		}
	}
	root["$comment"] = "Printed by actionlint config schema from " + RootFile + " and the tool providers that plugins install."
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
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
func addTools(tools object, manifests []provider.Manifest) error {
	entries, _ := tools["properties"].(object)
	unknown, _ := tools["additionalProperties"].(object)
	for _, manifest := range manifests {
		name := manifest.Rule.Name
		if _, bundled := entries[name]; bundled {
			continue
		}
		entry, err := toolEntry(unknown, manifest)
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
func toolEntry(unknown object, manifest provider.Manifest) (object, error) {
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
		settings, err := settingsSchema(manifest)
		if err != nil {
			return nil, err
		}
		values := []any{settings, object{"type": "null"}}
		if manifest.ReadsConfigFiles() {
			values = append([]any{object{"type": "string", "minLength": json.Number("1")}}, values...)
		}
		delete(config, "oneOf")
		config["anyOf"] = values
		return copied, nil
	}
	return nil, errors.New("the schema for unknown tools has no mapping alternative")
}

// settingsSchema returns the schema of inline tools.<name>.config settings:
// a mapping valid against the manifest's config_schema and its process
// transport's config_schema. Without either, any mapping is valid.
func settingsSchema(manifest provider.Manifest) (object, error) {
	documents := []json.RawMessage{manifest.ConfigSchema}
	if manifest.Process != nil {
		documents = append(documents, manifest.Process.ConfigSchema)
	}
	settings := object{"type": "object"}
	var all []any
	for _, document := range documents {
		if len(document) == 0 {
			continue
		}
		schema, err := decodeObject(document, "config_schema")
		if err != nil {
			return nil, err
		}
		embedded(schema)
		all = append(all, schema)
	}
	if len(all) != 0 {
		settings["allOf"] = all
	}
	return settings, nil
}

// deepCopy returns a copy of value that shares no storage with it.
func deepCopy(value object) (object, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return decodeObject(data, "schema")
}
