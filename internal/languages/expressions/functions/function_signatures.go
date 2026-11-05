package functions

import (
	"fmt"
	"strings"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

// FunctionEvaluationPolicy describes argument demand independently of return types.
type FunctionEvaluationPolicy uint8

const (
	EvaluateAllArguments FunctionEvaluationPolicy = iota
	EvaluateStringPredicate
	EvaluateContains
	EvaluateJoin
	EvaluateFormat
	EvaluateCase
)

// FuncSignature is a signature of function, which holds return and arguments types.
type FuncSignature struct {
	// Name is a name of the function.
	Name string
	// Ret declares the function's return domain, including for invalid calls.
	// A known result can refine this domain without replacing the declaration.
	Ret exprtypes.Type
	// Params describes the types consumed after parameter conversion. Accepted
	// source types, conversion outcomes and converted values are recorded separately
	// for each argument. The final parameter repeats for variadic signatures.
	Params []exprtypes.Type
	// VariableLengthParams is a flag to handle variable length parameters. When this flag is set to
	// true, it means that the last type of params might be specified multiple times (including zero
	// times). Setting true implies length of Params is more than 0.
	VariableLengthParams bool
	// MaxParams is a parser limit. Zero means the signature has no upper limit.
	MaxParams int
	// IsConstFunc is true when the function returns a constant when all parameters are constants.
	IsConstFunc bool
	// CompositeStringFallback accepts composite values without string conversion.
	CompositeStringFallback bool
	// EvaluationPolicy describes which arguments the function evaluates.
	EvaluationPolicy FunctionEvaluationPolicy
}

func (sig *FuncSignature) String() string {
	ts := make([]string, 0, len(sig.Params))
	for _, p := range sig.Params {
		ts = append(ts, p.String())
	}
	elip := ""
	if sig.VariableLengthParams {
		elip = "..."
	}
	return fmt.Sprintf("%s(%s%s) -> %s", sig.Name, strings.Join(ts, ", "), elip, sig.Ret.String())
}

// Builtins declares function signatures and their supported constant implementations.
// Keys use lowercase names because function lookup ignores case.
// https://docs.github.com/en/actions/learn-github-actions/expressions#functions
var Builtins = map[string]Function{
	"contains": {Signatures: []*FuncSignature{
		{
			Name:                    "contains",
			EvaluationPolicy:        EvaluateContains,
			CompositeStringFallback: true,
			Ret:                     exprtypes.BoolType(),
			Params: []exprtypes.Type{
				exprtypes.StringType(),
				exprtypes.StringType(),
			},
			IsConstFunc: true,
		},
		{
			Name:                    "contains",
			EvaluationPolicy:        EvaluateContains,
			CompositeStringFallback: true,
			Ret:                     exprtypes.BoolType(),
			Params: []exprtypes.Type{
				exprtypes.NewArrayType(exprtypes.AnyType()).Type(),
				exprtypes.AnyType(),
			},
			IsConstFunc: true,
		},
	}, implementation: evaluateContains},
	"startswith": {Signatures: []*FuncSignature{{
		Name:                    "startsWith",
		EvaluationPolicy:        EvaluateStringPredicate,
		CompositeStringFallback: true,
		Ret:                     exprtypes.BoolType(),
		Params: []exprtypes.Type{
			exprtypes.StringType(),
			exprtypes.StringType(),
		},
		IsConstFunc: true,
	}}, implementation: evaluateStartsWith},
	"endswith": {Signatures: []*FuncSignature{{
		Name:                    "endsWith",
		EvaluationPolicy:        EvaluateStringPredicate,
		CompositeStringFallback: true,
		Ret:                     exprtypes.BoolType(),
		Params: []exprtypes.Type{
			exprtypes.StringType(),
			exprtypes.StringType(),
		},
		IsConstFunc: true,
	}}, implementation: evaluateEndsWith},
	"format": {Signatures: []*FuncSignature{{
		Name:             "format",
		EvaluationPolicy: EvaluateFormat,
		MaxParams:        255,
		Ret:              exprtypes.StringType(),
		Params:           []exprtypes.Type{exprtypes.StringType()},
		IsConstFunc:      true,
	}, {
		Name:             "format",
		EvaluationPolicy: EvaluateFormat,
		MaxParams:        255,
		Ret:              exprtypes.StringType(),
		Params: []exprtypes.Type{
			exprtypes.StringType(),
			exprtypes.AnyType(), // variable length
		},
		VariableLengthParams: true,
		IsConstFunc:          true,
	}}, implementation: evaluateFormat, inspect: inspectFormat},
	"join": {Signatures: []*FuncSignature{
		{
			Name:                    "join",
			EvaluationPolicy:        EvaluateJoin,
			CompositeStringFallback: true,
			Ret:                     exprtypes.StringType(),
			Params:                  []exprtypes.Type{exprtypes.StringType()},
			IsConstFunc:             true,
		},
		{
			Name:                    "join",
			EvaluationPolicy:        EvaluateJoin,
			CompositeStringFallback: true,
			Ret:                     exprtypes.StringType(),
			Params:                  []exprtypes.Type{exprtypes.StringType(), exprtypes.StringType()},
			IsConstFunc:             true,
		},
		{
			Name:                    "join",
			EvaluationPolicy:        EvaluateJoin,
			CompositeStringFallback: true,
			Ret:                     exprtypes.StringType(),
			Params: []exprtypes.Type{
				exprtypes.NewArrayType(exprtypes.AnyType()).Type(),
				exprtypes.StringType(),
			},
			IsConstFunc: true,
		},
		// When the second parameter is omitted, values are concatenated with ','.
		{
			Name:                    "join",
			EvaluationPolicy:        EvaluateJoin,
			CompositeStringFallback: true,
			Ret:                     exprtypes.StringType(),
			Params: []exprtypes.Type{
				exprtypes.NewArrayType(exprtypes.AnyType()).Type(),
			},
			IsConstFunc: true,
		},
	}, implementation: evaluateJoin},
	"tojson": {Signatures: []*FuncSignature{{
		Name: "toJSON",
		Ret:  exprtypes.StringType(),
		Params: []exprtypes.Type{
			exprtypes.AnyType(),
		},
		IsConstFunc: true,
	}}, implementation: evaluateToJSON},
	"fromjson": {Signatures: []*FuncSignature{{
		Name: "fromJSON",
		Ret:  exprtypes.AnyType(),
		Params: []exprtypes.Type{
			exprtypes.StringType(),
		},
		IsConstFunc: true,
	}}, implementation: evaluateFromJSON, inspect: inspectFromJSON},
	"hashfiles": {Signatures: []*FuncSignature{{
		Name:      "hashFiles",
		MaxParams: 255,
		Ret:       exprtypes.StringType(),
		Params: []exprtypes.Type{
			exprtypes.StringType(),
		},
		VariableLengthParams: true,
	}}},
	"success": {Signatures: []*FuncSignature{{
		Name:   "success",
		Ret:    exprtypes.BoolType(),
		Params: []exprtypes.Type{},
	}}},
	"always": {Signatures: []*FuncSignature{{
		Name:   "always",
		Ret:    exprtypes.BoolType(),
		Params: []exprtypes.Type{},
	}}},
	"cancelled": {Signatures: []*FuncSignature{{
		Name:   "cancelled",
		Ret:    exprtypes.BoolType(),
		Params: []exprtypes.Type{},
	}}},
	"failure": {Signatures: []*FuncSignature{{
		Name:   "failure",
		Ret:    exprtypes.BoolType(),
		Params: []exprtypes.Type{},
	}}},
	"case": {Signatures: []*FuncSignature{{
		Name:             "case",
		EvaluationPolicy: EvaluateCase,
		MaxParams:        255,
		Ret:              exprtypes.AnyType(),
		Params: []exprtypes.Type{
			exprtypes.BoolType(),
			exprtypes.AnyType(),
			exprtypes.AnyType(),
		},
		VariableLengthParams: true,
		IsConstFunc:          true,
	}}, implementation: evaluateCase, inspect: inspectCase},
}

// Global variables

// ParameterAt returns the consumed type at index for a call with count arguments.
// The caller validates argument count before requesting a parameter.
func (sig *FuncSignature) ParameterAt(index, count int) exprtypes.Type {
	if sig.EvaluationPolicy == EvaluateCase {
		if index < count-1 && index%2 == 0 {
			return exprtypes.BoolType()
		}
		return exprtypes.AnyType()
	}
	return sig.Params[min(index, len(sig.Params)-1)]
}

// AcceptsArgumentCount checks arity and parser limits without evaluating arguments.
func (sig *FuncSignature) AcceptsArgumentCount(count int) bool {
	if sig.MaxParams > 0 && count > sig.MaxParams {
		return false
	}
	if sig.VariableLengthParams {
		if count < len(sig.Params) {
			return false
		}
	} else if count != len(sig.Params) {
		return false
	}
	return sig.EvaluationPolicy != EvaluateCase || count%2 == 1
}
