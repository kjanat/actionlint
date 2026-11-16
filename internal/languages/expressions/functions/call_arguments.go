package functions

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

// ArgumentValueState distinguishes an unknown value from a rejected conversion.
type ArgumentValueState uint8

const (
	ArgumentUnknown ArgumentValueState = iota
	ArgumentKnown
	ArgumentInvalid
)

type CallArgument struct {
	Source types.Type
	Value  any
	State  ArgumentValueState
	Demand ArgumentDemand
	Format *FormatPlan
}

// ParameterMatch records the source argument, parameter contract and converted value.
type ParameterMatch struct {
	Argument   CallArgument
	Accepted   bool
	Parameter  types.Type
	Conversion ArgumentConversion
	Converted  types.Type
	Value      any
	State      ArgumentValueState
}

func MatchParameter(parameter types.Type, arg CallArgument, compositeFallback bool) ParameterMatch {
	m := ParameterMatch{Argument: arg, Accepted: true, Parameter: parameter, Converted: arg.Source, Value: arg.Value, State: arg.State}
	actual := arg.Source
	if actual.Kind() == types.KindAny && arg.State == ArgumentKnown {
		actual = types.TypeOfJSONValue(arg.Value)
	}
	if parameter.Kind() == types.KindString {
		switch actual.Kind() {
		case types.KindAny, types.KindString:
			return m
		case types.KindBool, types.KindNull, types.KindNumber, types.KindArray, types.KindObject:
			if compositeFallback && (actual.Kind() == types.KindArray || actual.Kind() == types.KindObject) {
				m.Conversion = ConversionUnavailable
				return m
			}
			m.Conversion = ConversionString
			m.Converted = types.StringType()
			// Arrays convert to "Array" and objects to "Object", even when their contents are unknown.
			if m.State == ArgumentUnknown {
				if actual.Kind() == types.KindArray {
					m.Value, m.State = "Array", ArgumentKnown
				} else if actual.Kind() == types.KindObject {
					m.Value, m.State = "Object", ArgumentKnown
				}
			}
			if m.State == ArgumentKnown {
				if value, ok := values.FunctionString(m.Value); ok {
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
	if parameter.Kind() != types.KindAny && actual.Kind() != types.KindAny &&
		(parameter.Kind() != actual.Kind() || (parameter.Kind() == types.KindObject && !parameter.Assignable(actual))) {
		m.Accepted = false
		m.State = ArgumentInvalid
	}
	return m
}
