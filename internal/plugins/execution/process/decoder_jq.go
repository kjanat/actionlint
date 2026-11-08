package process

import (
	"context"
	_ "embed"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"sync"

	"actionlint.kjanat.dev/v2/internal/plugins/execution/jq"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// JQInput selects what a jq transform receives as its input value.
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

// JQ reshapes tool output with a jq program run by gojq. The program sees
// stdout as its input and the check's argument variables as $vars. It must
// emit exactly one value: a diagnostics array, or an object with diagnostics
// and optional sources and errors, as result_schema.json defines.
//
// Programs are sandboxed: $ENV and env are empty objects, import, include,
// input and inputs do not compile, evaluation stops when ctx is done, and the
// output is bounded by MaxOutputBytes and MaxDiagnostics.
type JQ struct {
	program *jq.Program
	input   JQInput
}

// NewJQ compiles a transform program.
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

// stringVariables converts argument variables to jq values.
func stringVariables(vars map[string]string) map[string]any {
	variables := make(map[string]any, len(vars))
	for name, value := range vars {
		variables[name] = value
	}
	return variables
}

// Decode implements Decoder.
func (j *JQ) Decode(ctx context.Context, stdout []byte, vars map[string]string) (Result, error) {
	if err := checkInput(stdout); err != nil {
		return Result{}, err
	}
	var input any
	switch j.input {
	case JQJSON:
		if err := jsonv2.Unmarshal(stdout, &input); err != nil {
			return Result{}, fmt.Errorf("decode tool output: %w", err)
		}
	case JQText:
		input = string(stdout)
	}
	output, err := j.program.Apply(ctx, input, stringVariables(vars))
	if err != nil {
		return Result{}, err
	}
	return decodeTransformOutput(output)
}

// decodeTransformOutput validates transform output against the result schema
// before decoding it, so a malformed program fails instead of dropping data.
func decodeTransformOutput(data []byte) (Result, error) {
	schema, err := resultSchema()
	if err != nil {
		return Result{}, fmt.Errorf("transform result schema: %w", err)
	}
	var document any
	if err := jsonv2.Unmarshal(data, &document); err != nil {
		return Result{}, fmt.Errorf("decode jq transform output: %w", err)
	}
	if err := schema.Validate(document); err != nil {
		return Result{}, fmt.Errorf("invalid jq transform output: %w", err)
	}
	var result Result
	strict := jsonv2.RejectUnknownMembers(true)
	if jsontext.Value(data).Kind() == '[' {
		err = jsonv2.Unmarshal(data, &result.Diagnostics, strict)
	} else {
		err = jsonv2.Unmarshal(data, &result, strict)
	}
	if err != nil {
		return Result{}, fmt.Errorf("decode jq transform output: %w", err)
	}
	if err := checkCount(len(result.Diagnostics)); err != nil {
		return Result{}, err
	}
	return result, nil
}
