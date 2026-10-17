package analyzerhost

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ValidateOptions checks provider options independently of transport. Schemas must bundle their refs;
// loading a plugin must never grant implicit filesystem or network reads.
func ValidateOptions(schemaJSON, optionsJSON json.RawMessage) error {
	if len(schemaJSON) == 0 {
		if len(optionsJSON) != 0 && !json.Valid(optionsJSON) {
			return errors.New("invalid options JSON")
		}
		return nil
	}
	var document, options any
	if err := jsonv2.Unmarshal(schemaJSON, &document); err != nil {
		return fmt.Errorf("invalid options schema: %w", err)
	}
	if len(optionsJSON) == 0 {
		optionsJSON = json.RawMessage(`{}`)
	}
	if err := jsonv2.Unmarshal(optionsJSON, &options); err != nil {
		return fmt.Errorf("invalid options JSON: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(bundledSchemaLoader{})
	const name = "https://actionlint.invalid/plugin-options.json"
	if err := compiler.AddResource(name, document); err != nil {
		return err
	}
	schema, err := compiler.Compile(name)
	if err != nil {
		return fmt.Errorf("invalid options schema: %w", err)
	}
	return schema.Validate(options)
}

type bundledSchemaLoader struct{}

func (bundledSchemaLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("options schema reference %q must be bundled", url)
}
