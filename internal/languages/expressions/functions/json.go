package functions

import (
	"errors"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/values"
	"actionlint.kjanat.dev/v2/internal/languages/json"
)

func evaluateFromJSON(context EvaluationContext) values.EvaluationResult {
	argument := context.Argument(0)
	if argument.State != values.EvaluationKnown {
		return argument
	}
	text, known := values.FunctionString(argument.Value)
	if !known {
		return unknownValue()
	}
	switch read := readFunctionJSON(text).(type) {
	case InvalidJSON:
		return invalidValue()
	case unsupportedFunctionJSON:
		return unknownValue()
	case decodedFunctionJSON:
		parsed := read.parsed
		// Parsing modes disagree about duplicate members and the sign of integer -0.
		if len(parsed.Collisions) > 0 || parsed.IntegerNegativeZero {
			return unknownValue()
		}
		return knownValue(parsed.Value)
	}
	return unknownValue()
}

//sumtype:decl
type functionJSONRead interface{ functionJSONRead() }

type decodedFunctionJSON struct{ parsed json.ReadResult }

func (decodedFunctionJSON) functionJSONRead() {}

type unsupportedFunctionJSON struct{ parsed json.ReadResult }

func (unsupportedFunctionJSON) functionJSONRead() {}

func (InvalidJSON) functionJSONRead() {}

func readFunctionJSON(text string) functionJSONRead {
	parsed, err := json.ParseJSON(text)
	if errors.Is(err, json.ErrUnsupportedValue) {
		return unsupportedFunctionJSON{parsed: parsed}
	}
	if err != nil {
		failure := InvalidJSON{Message: err.Error()}
		if syntax, ok := errors.AsType[*json.ReadError](err); ok {
			failure.Offset = &syntax.Offset
		}
		return failure
	}
	return decodedFunctionJSON{parsed: parsed}
}

func evaluateToJSON(context EvaluationContext) values.EvaluationResult {
	argument := context.Argument(0)
	if argument.State != values.EvaluationKnown {
		return argument
	}
	switch value := argument.Value.(type) {
	case nil:
		return knownValue("null")
	case bool:
		return knownValue(strconv.FormatBool(value))
	case string:
		// Only fold text with identical escaping in the supported JSON engines.
		for _, r := range value {
			if r < ' ' || r > '~' || strings.ContainsRune("\"\\<>&'+`", r) {
				return unknownValue()
			}
		}
		return knownValue(`"` + value + `"`)
	default:
		return unknownValue()
	}
}
