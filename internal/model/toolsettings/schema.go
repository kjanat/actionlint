// Package toolsettings describes the settings a tool provider accepts under tools.<name>.
// It imports no actionlint package, which keeps configuration decoding independent of the plugin layer.
package toolsettings

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// location is never fetched.
const location = "https://actionlint.invalid/provider-config.json"

type Schema struct {
	schema *jsonschema.Schema
}

type Failure struct {
	Path        string
	Message     string
	UnknownKeys []string
}

type ValidationError struct{ Failures []Failure }

func (e *ValidationError) Error() string {
	problems := make([]string, 0, len(e.Failures))
	for _, failure := range e.Failures {
		at := failure.Path
		if at == "" {
			at = "/"
		}
		problems = append(problems, fmt.Sprintf("at %s: %s", at, failure.Message))
	}
	return strings.Join(problems, "; ")
}

// Compile requires bundled definitions and refuses every external reference.
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

func (s *Schema) Validate(document []byte) error {
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(document))
	if err != nil {
		return err
	}
	err = s.schema.Validate(value)
	if err == nil {
		return nil
	}
	invalid, ok := errors.AsType[*jsonschema.ValidationError](err)
	if !ok {
		return errors.New(err.Error())
	}
	result := new(ValidationError)
	output := invalid.BasicOutput()
	units := output.Errors
	if output.Error != nil {
		units = append(units, *output)
	}
	var problems []string
	for _, unit := range units {
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
			failure := Failure{Path: unit.InstanceLocation, Message: unit.Error.String()}
			if unknown, ok := unit.Error.Kind.(*kind.AdditionalProperties); ok {
				failure.UnknownKeys = slices.Clone(unknown.Properties)
			}
			result.Failures = append(result.Failures, failure)
		}
	}
	if len(problems) == 0 {
		return errors.New(err.Error())
	}
	return result
}

type refuseLoader struct{}

func (refuseLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("schema reference %q must be bundled", url)
}
