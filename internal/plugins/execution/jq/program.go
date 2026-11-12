// Package jq compiles and runs bounded jq transformations supplied by plugins.
package jq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/itchyny/gojq"
)

// ApplyJSON decodes input without rounding integer values before evaluation.
func (p *Program) ApplyJSON(ctx context.Context, input []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return p.Apply(ctx, value, map[string]any{})
}

// Program evaluates jq without filesystem, environment or network access.
type Program struct {
	code           *gojq.Code
	maxOutputBytes int
}

func New(program string, maxOutputBytes int) (*Program, error) {
	query, err := gojq.Parse(program)
	if err != nil {
		return nil, fmt.Errorf("parse jq transform: %w", err)
	}
	// No environ loader, module loader or input iterator: each stays closed.
	code, err := gojq.Compile(query, gojq.WithVariables([]string{"$vars"}))
	if err != nil {
		return nil, fmt.Errorf("compile jq transform: %w", err)
	}
	return &Program{code: code, maxOutputBytes: maxOutputBytes}, nil
}

// Apply returns the program's single JSON value. It stops when ctx is cancelled
// and rejects multiple values or output exceeding the configured byte limit.
func (p *Program) Apply(ctx context.Context, input any, variables map[string]any) ([]byte, error) {
	evaluation, release := context.WithCancel(ctx)
	defer release()
	iter := p.code.RunWithContext(evaluation, jsonValue(input), jsonValue(variables))
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
		output, ok = encodeBounded(value, p.maxOutputBytes)
		if !ok {
			return nil, fmt.Errorf("jq transform output exceeds %d bytes", p.maxOutputBytes)
		}
	}
}

// encodeBounded rejects output exceeding the caller's byte limit.
func encodeBounded(value any, maxOutputBytes int) ([]byte, bool) {
	encoded, err := gojq.Marshal(value)
	if err != nil || len(encoded) > maxOutputBytes {
		return nil, false
	}
	return encoded, true
}

// jsonValue converts decoded JSON numbers to the integer, big integer or float
// values gojq accepts.
func jsonValue(value any) any {
	switch value := value.(type) {
	case json.Number:
		if integer, err := strconv.Atoi(value.String()); err == nil {
			return integer
		}
		if integer, ok := new(big.Int).SetString(value.String(), 10); ok {
			return integer
		}
		float, err := value.Float64()
		if err != nil {
			return value.String()
		}
		return float
	case map[string]any:
		converted := make(map[string]any, len(value))
		for key, item := range value {
			converted[key] = jsonValue(item)
		}
		return converted
	case []any:
		converted := make([]any, len(value))
		for i, item := range value {
			converted[i] = jsonValue(item)
		}
		return converted
	default:
		return value
	}
}
