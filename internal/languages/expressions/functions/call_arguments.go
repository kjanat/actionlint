package functions

import (
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

// ArgumentValueState distinguishes an unknown value from a rejected conversion.
type ArgumentValueState uint8

const (
	ArgumentUnknown ArgumentValueState = iota
	ArgumentKnown
	ArgumentInvalid
)

type CallArgument struct {
	Source exprtypes.Type
	Value  any
	State  ArgumentValueState
	Demand ArgumentDemand
	Format *FormatPlan
}

// ParameterMatch records the source argument, parameter contract and converted value.
type ParameterMatch struct {
	Argument   CallArgument
	Accepted   bool
	Parameter  exprtypes.Type
	Conversion ArgumentConversion
	Converted  exprtypes.Type
	Value      any
	State      ArgumentValueState
}

func MatchParameter(parameter exprtypes.Type, arg CallArgument, compositeFallback bool) ParameterMatch {
	m := ParameterMatch{Argument: arg, Accepted: true, Parameter: parameter, Converted: arg.Source, Value: arg.Value, State: arg.State}
	actual := arg.Source
	if actual.Kind() == exprtypes.KindAny && arg.State == ArgumentKnown {
		actual = exprtypes.TypeOfJSONValue(arg.Value)
	}
	if parameter.Kind() == exprtypes.KindString {
		switch actual.Kind() {
		case exprtypes.KindAny, exprtypes.KindString:
			return m
		case exprtypes.KindBool, exprtypes.KindNull, exprtypes.KindNumber, exprtypes.KindArray, exprtypes.KindObject:
			if compositeFallback && (actual.Kind() == exprtypes.KindArray || actual.Kind() == exprtypes.KindObject) {
				m.Conversion = ConversionUnavailable
				return m
			}
			m.Conversion = ConversionString
			m.Converted = exprtypes.StringType()
			// Arrays convert to "Array" and objects to "Object", even when their contents are unknown.
			if m.State == ArgumentUnknown {
				switch actual.Kind() {
				case exprtypes.KindArray:
					m.Value, m.State = "Array", ArgumentKnown
				case exprtypes.KindObject:
					m.Value, m.State = "Object", ArgumentKnown
				}
			}
			if m.State == ArgumentKnown {
				if value, ok := exprvalues.FunctionString(m.Value); ok {
					m.Value = value
				} else {
					m.Value, m.State = nil, ArgumentUnknown
				}
			}
			return m
		}
	}
	if p, ok := parameter.Array(); ok {
		if a, ok := actual.Array(); ok {
			// Array parameters consume the original elements. Scalar string
			// conversion does not imply that an entire array is normalized.
			elem := MatchParameter(p.Elem(), CallArgument{Source: a.Elem()}, false)
			if !elem.Accepted || elem.Conversion != ConversionNone {
				m.Accepted = false
				m.State = ArgumentInvalid
			}
			return m
		}
	}
	// Function acceptance is a separate contract from Type.Assignable,
	// which also includes expression-level boolean and string compatibility.
	if parameter.Kind() != exprtypes.KindAny && actual.Kind() != exprtypes.KindAny &&
		(parameter.Kind() != actual.Kind() || (parameter.Kind() == exprtypes.KindObject && !parameter.Assignable(actual))) {
		m.Accepted = false
		m.State = ArgumentInvalid
	}
	return m
}
