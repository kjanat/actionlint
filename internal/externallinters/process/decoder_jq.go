package process

import (
	"context"
	_ "embed"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"sync"

	"actionlint.kjanat.dev/v2/internal/externallinters/jq"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type JQInput string

const (
	// JQJSON parses stdout as one JSON value.
	JQJSON JQInput = "json"
	// JQText passes stdout as one string.
	JQText JQInput = "text"
)

//go:embed result_schema.json
var resultSchemaJSON []byte

var resultSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	const url = "https://actionlint.kjanat.dev/schemas/tool-provider-transform-result-1.json"
	var document any
	if err := jsonv2.Unmarshal(resultSchemaJSON, &document); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(url, document); err != nil {
		return nil, err
	}
	return compiler.Compile(url)
})

// JQ programs see the check's argument variables as $vars and must emit
// exactly one value matching result_schema.json.
//
// In the sandbox, $ENV and env are empty objects, import, include, input and
// inputs fail to compile, and evaluation stops when ctx is done.
type JQ struct {
	program *jq.Program
	input   JQInput
}

func NewJQ(program string, input JQInput) (*JQ, error) {
	if input != JQJSON && input != JQText {
		return nil, fmt.Errorf("unknown jq input %q", input)
	}
	compiled, err := compileJQ(program)
	if err != nil {
		return nil, err
	}
	return &JQ{program: compiled, input: input}, nil
}

func compileJQ(program string) (*jq.Program, error) {
	return jq.New(program, MaxOutputBytes)
}

func stringVariables(vars map[string]string) map[string]any {
	variables := make(map[string]any, len(vars))
	for name, value := range vars {
		variables[name] = value
	}
	return variables
}

func (j *JQ) Decode(ctx context.Context, stdout []byte, vars map[string]string) (DecodedCheck, error) {
	if err := checkInput(stdout); err != nil {
		return DecodedCheck{}, err
	}
	var input any
	switch j.input {
	case JQJSON:
		if err := jsonv2.Unmarshal(stdout, &input); err != nil {
			return DecodedCheck{}, fmt.Errorf("decode tool output: %w", err)
		}
	case JQText:
		input = string(stdout)
	}
	output, err := j.program.Apply(ctx, input, stringVariables(vars))
	if err != nil {
		return DecodedCheck{}, err
	}
	return decodeJQResult(output)
}

func decodeJQResult(data []byte) (DecodedCheck, error) {
	schema, err := resultSchema()
	if err != nil {
		return DecodedCheck{}, fmt.Errorf("transform result schema: %w", err)
	}
	var document any
	if err := jsonv2.Unmarshal(data, &document); err != nil {
		return DecodedCheck{}, fmt.Errorf("decode jq transform output: %w", err)
	}
	if err := schema.Validate(document); err != nil {
		return DecodedCheck{}, fmt.Errorf("invalid jq transform output: %w", err)
	}
	var result DecodedCheck
	strict := jsonv2.RejectUnknownMembers(true)
	if jsontext.Value(data).Kind() == '[' {
		err = jsonv2.Unmarshal(data, &result.Diagnostics, strict)
	} else {
		err = jsonv2.Unmarshal(data, &result, strict)
	}
	if err != nil {
		return DecodedCheck{}, fmt.Errorf("decode jq transform output: %w", err)
	}
	if err := checkCount(len(result.Diagnostics)); err != nil {
		return DecodedCheck{}, err
	}
	return result, nil
}
