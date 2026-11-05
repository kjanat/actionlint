package functions

import (
	"errors"
	"strconv"
	"strings"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
	runnerjson "actionlint.kjanat.dev/v2/internal/languages/json"
)

func evaluateFromJSON(context EvaluationContext) exprvalues.EvaluationResult {
	argument := context.Argument(0)
	if argument.State != exprvalues.EvaluationKnown {
		return argument
	}
	text, known := exprvalues.FunctionString(argument.Value)
	if !known {
		return unknownValue()
	}
	parsed, err := runnerjson.Parse(text)
	if errors.Is(err, runnerjson.ErrUnsupportedValue) {
		return unknownValue()
	}
	if err != nil {
		return invalidValue()
	}
	// Parsing modes disagree about duplicate members and the sign of integer -0.
	if len(parsed.Collisions) > 0 || parsed.IntegerNegativeZero {
		return unknownValue()
	}
	return knownValue(parsed.Value)
}

func evaluateToJSON(context EvaluationContext) exprvalues.EvaluationResult {
	argument := context.Argument(0)
	if argument.State != exprvalues.EvaluationKnown {
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
