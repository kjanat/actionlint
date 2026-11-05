package process

import (
	"context"
	_ "embed"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"sync"

	"github.com/itchyny/gojq"
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
	program *jqProgram
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

// jqProgram is one sandboxed, compiled jq program taking $vars.
type jqProgram struct{ code *gojq.Code }

func compileJQ(program string) (*jqProgram, error) {
	query, err := gojq.Parse(program)
	if err != nil {
		return nil, fmt.Errorf("parse jq transform: %w", err)
	}
	// No environ loader, module loader or input iterator: each stays closed.
	code, err := gojq.Compile(query, gojq.WithVariables([]string{"$vars"}))
	if err != nil {
		return nil, fmt.Errorf("compile jq transform: %w", err)
	}
	return &jqProgram{code: code}, nil
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
	output, err := j.program.run(ctx, input, stringVariables(vars))
	if err != nil {
		return Result{}, err
	}
	return decodeTransformOutput(output)
}

// run evaluates the program and returns its single output value as JSON.
// Errors retain no evaluator state: a cancellation is reported as ctx's error
// and other failures by their message. The evaluator runs under a child of
// ctx, released on return, and never sees ctx itself.
func (p *jqProgram) run(ctx context.Context, input any, variables map[string]any) ([]byte, error) {
	evaluation, release := context.WithCancel(ctx)
	defer release()
	iter := p.code.RunWithContext(evaluation, input, variables)
	var output []byte
	for count := 0; ; count++ {
		value, ok := iter.Next()
		if !ok {
			if count == 0 {
				return nil, errors.New("jq transform produced no value")
			}
			return output, nil
		}
		if err, failed := value.(error); failed {
			if cause := ctx.Err(); cause != nil {
				return nil, fmt.Errorf("jq transform: %w", cause)
			}
			return nil, errors.New("jq transform: " + err.Error())
		}
		if count > 0 {
			return nil, errors.New("jq transform produced more than one value")
		}
		output, ok = encodeBounded(value)
		if !ok {
			return nil, fmt.Errorf("jq transform output exceeds %d bytes", MaxOutputBytes)
		}
	}
}

// encodeBounded marshals one jq value, refusing output over MaxOutputBytes.
func encodeBounded(value any) ([]byte, bool) {
	encoded, err := gojq.Marshal(value)
	if err != nil || len(encoded) > MaxOutputBytes {
		return nil, false
	}
	return encoded, true
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
