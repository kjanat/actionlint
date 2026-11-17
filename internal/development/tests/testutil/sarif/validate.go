// Package sarif validates test output against the OASIS SARIF schema.
package sarif

import (
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed testdata/sarif-schema-2.1.0.json
var schemaJSON []byte

var compileSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	var document any
	if err := json.Unmarshal(schemaJSON, &document); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	compiler.UseLoader(jsonschema.SchemeURLLoader{})
	// The validator's URI parser accepts unescaped spaces. Retain its format
	// checks and reject whitespace before passing URI references to it.
	uriCompiler := jsonschema.NewCompiler()
	const uriLocation = "urn:actionlint:test:uri-reference"
	if err := uriCompiler.AddResource(uriLocation, map[string]any{"type": "string", "format": "uri-reference"}); err != nil {
		return nil, err
	}
	uriCompiler.AssertFormat()
	uriSchema, err := uriCompiler.Compile(uriLocation)
	if err != nil {
		return nil, err
	}
	compiler.RegisterFormat(&jsonschema.Format{Name: "uri-reference", Validate: func(value any) error {
		if text, ok := value.(string); ok && strings.ContainsAny(text, " \t\r\n") {
			return errors.New("URI references must percent-encode whitespace")
		}
		return uriSchema.Validate(value)
	}})
	const location = "https://docs.oasis-open.org/sarif/sarif/v2.1.0/errata01/os/schemas/sarif-schema-2.1.0.json"
	if err := compiler.AddResource(location, document); err != nil {
		return nil, err
	}
	return compiler.Compile(location)
})

// Validate checks JSON against the OASIS SARIF 2.1.0 errata 01 schema, including URI formats.
func Validate(data []byte) error {
	schema, err := compileSchema()
	if err != nil {
		return err
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	return schema.Validate(document)
}

// RequireValid fails the test with the schema's validation errors.
func RequireValid(tb testing.TB, data []byte) {
	tb.Helper()
	if err := Validate(data); err != nil {
		tb.Fatalf("invalid SARIF: %v", err)
	}
}
