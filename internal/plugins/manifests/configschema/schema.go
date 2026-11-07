// Package configschema compiles and applies the JSON schemas tool providers
// declare for tools.<name>.config. It imports no actionlint package, so
// configuration decoding can validate tool settings without depending on any
// tool host.
package configschema

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// location identifies every schema while it compiles. The address is never
// fetched.
const location = "https://actionlint.invalid/provider-config.json"

// Schema validates tools.<name>.config values for one provider.
type Schema struct {
	schema *jsonschema.Schema
}

// Compile compiles a configuration schema. Schemas must bundle their
// definitions: every external reference is refused, so compiling never reads
// files or the network.
func Compile(document []byte) (*Schema, error) {
	var value any
	if err := jsonv2.Unmarshal(document, &value); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(refuseLoader{})
	if err := compiler.AddResource(location, value); err != nil {
		return nil, err
	}
	schema, err := compiler.Compile(location)
	if err != nil {
		return nil, err
	}
	return &Schema{schema: schema}, nil
}

// Validate checks one JSON document against the schema. The error names each
// failing location within the document, never the schema's internal address.
func (s *Schema) Validate(document []byte) error {
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(document))
	if err != nil {
		return err
	}
	err = s.schema.Validate(value)
	if err == nil {
		return nil
	}
	// Report text only: a validation error references the compiled schema.
	invalid, ok := errors.AsType[*jsonschema.ValidationError](err)
	if !ok {
		return errors.New(err.Error())
	}
	var problems []string
	for _, unit := range invalid.BasicOutput().Errors {
		if unit.Error == nil {
			continue
		}
		at := unit.InstanceLocation
		if at == "" {
			at = "/"
		}
		problem := fmt.Sprintf("at %s: %s", at, unit.Error)
		if !slices.Contains(problems, problem) {
			problems = append(problems, problem)
		}
	}
	if len(problems) == 0 {
		return errors.New(err.Error())
	}
	return errors.New(strings.Join(problems, "; "))
}

// refuseLoader refuses every external reference.
type refuseLoader struct{}

func (refuseLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("schema reference %q must be bundled", url)
}
