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

// BuiltinFuncSignatures is a set of all builtin function signatures. All function names are in
// lower case because function names are compared in case insensitive.
// https://docs.github.com/en/actions/learn-github-actions/expressions#functions
var BuiltinFuncSignatures = map[string][]*FuncSignature{
	"contains": {
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
	},
	"startswith": {{
		Name:                    "startsWith",
		EvaluationPolicy:        EvaluateStringPredicate,
		CompositeStringFallback: true,
		Ret:                     exprtypes.BoolType(),
		Params: []exprtypes.Type{
			exprtypes.StringType(),
			exprtypes.StringType(),
		},
		IsConstFunc: true,
	}},
	"endswith": {{
		Name:                    "endsWith",
		EvaluationPolicy:        EvaluateStringPredicate,
		CompositeStringFallback: true,
		Ret:                     exprtypes.BoolType(),
		Params: []exprtypes.Type{
			exprtypes.StringType(),
			exprtypes.StringType(),
		},
		IsConstFunc: true,
	}},
	"format": {{
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
	}},
	"join": {
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
	},
	"tojson": {{
		Name: "toJSON",
		Ret:  exprtypes.StringType(),
		Params: []exprtypes.Type{
			exprtypes.AnyType(),
		},
		IsConstFunc: true,
	}},
	"fromjson": {{
		Name: "fromJSON",
		Ret:  exprtypes.AnyType(),
		Params: []exprtypes.Type{
			exprtypes.StringType(),
		},
		IsConstFunc: true,
	}},
	"hashfiles": {{
		Name:      "hashFiles",
		MaxParams: 255,
		Ret:       exprtypes.StringType(),
		Params: []exprtypes.Type{
			exprtypes.StringType(),
		},
		VariableLengthParams: true,
	}},
	"success": {{
		Name:   "success",
		Ret:    exprtypes.BoolType(),
		Params: []exprtypes.Type{},
	}},
	"always": {{
		Name:   "always",
		Ret:    exprtypes.BoolType(),
		Params: []exprtypes.Type{},
	}},
	"cancelled": {{
		Name:   "cancelled",
		Ret:    exprtypes.BoolType(),
		Params: []exprtypes.Type{},
	}},
	"failure": {{
		Name:   "failure",
		Ret:    exprtypes.BoolType(),
		Params: []exprtypes.Type{},
	}},
	"case": {{
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
	}},
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
