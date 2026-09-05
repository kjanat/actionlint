// Generate an editor schema from the configuration types and their Go comments.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"

	"actionlint.kjanat.dev"
	"github.com/invopop/jsonschema"
)

func reflector() *jsonschema.Reflector {
	return &jsonschema.Reflector{
		FieldNameTag:               "yaml",
		RequiredFromJSONSchemaTags: true,
		DoNotReference:             true,
		Mapper:                     mapYAMLType,
	}
}

// mapYAMLType describes the wire format of types with custom UnmarshalYAML
// methods. Ordinary fields, including future config settings, use reflection.
func mapYAMLType(t reflect.Type) *jsonschema.Schema {
	switch t {
	case reflect.TypeFor[actionlint.IgnorePatterns]():
		// JSON Schema's regex format uses a different dialect from Go's regexp.
		return &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "string"}}
	case reflect.TypeFor[actionlint.DefaultPermissionsAssumption]():
		return &jsonschema.Schema{
			Type:    "string",
			Enum:    []any{"restricted", "permissive"},
			Default: "restricted",
		}
	case reflect.TypeFor[actionlint.JobTimeoutPolicy]():
		// The runtime type stores private state and accepts either a boolean or
		// this mapping. Keep this in sync with JobTimeoutPolicy.UnmarshalYAML.
		mapping := reflector().Reflect(struct {
			MaxMinutes float64 `yaml:"max-minutes" jsonschema:"exclusiveMinimum=0,description=Largest allowed job timeout in minutes."`
		}{})
		mapping.Version = ""
		mapping.ID = ""
		return &jsonschema.Schema{OneOf: []*jsonschema.Schema{{Type: "boolean"}, mapping}}
	default:
		return nil
	}
}

// generate runs from the repository root, as go generate does.
func generate() ([]byte, error) {
	r := reflector()
	if err := r.AddGoComments("actionlint.kjanat.dev", "config.go"); err != nil {
		return nil, fmt.Errorf("read config comments: %w", err)
	}
	s := r.Reflect(actionlint.Config{})
	s.ID = "https://raw.githubusercontent.com/kjanat/actionlint/master/actionlint.schema.json"
	s.Title = "actionlint configuration"
	s.Comments = "Generated from config.go by go generate -run generate-config-schema. DO NOT EDIT."
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode schema: %w", err)
	}
	return append(b, '\n'), nil
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/generate-config-schema OUTPUT")
		os.Exit(1)
	}
	b, err := generate()
	if err == nil {
		err = os.WriteFile(os.Args[1], b, 0644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
