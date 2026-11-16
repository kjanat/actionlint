package json_test

import (
	"math"
	"math/big"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/conformance/expressions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
	"actionlint.kjanat.dev/v2/internal/languages/json"
)

// Newtonsoft.Json parses hexadecimal and octal through Convert.ToInt64(text,
// base), which interprets bit 63 as the sign. Decimal JSON keeps its magnitude.
// References:
// https://github.com/actions/runner/blob/67f01c276e0a91d967ba499ce4cdc9a95efa0262/src/Sdk/Expressions/Sdk/Functions/FromJson.cs
// https://github.com/JamesNK/Newtonsoft.Json/blob/13.0.3/Src/Newtonsoft.Json/JsonTextReader.cs#L2164
// https://github.com/dotnet/runtime/blob/v8.0.31/src/libraries/System.Private.CoreLib/src/System/ParseNumbers.cs#L217
// https://github.com/dotnet/runtime/blob/v8.0.31/src/libraries/System.Runtime.Numerics/src/System/Numerics/NumericsHelpers.cs#L40
func TestFromJSONIntegerBoundariesAgainstRunner(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source string
		want         float64
		invalid      bool
	}{
		{"hex signed maximum", "0x7fffffffffffffff", float64(9223372036854775807), false},
		{"hex sign bit", "0x8000000000000000", float64(-9223372036854775808), false},
		{"hex all bits", "0xffffffffffffffff", -1, false},
		{"hex overflow", "0x10000000000000000", 0, true},
		{"octal signed maximum", "0777777777777777777777", float64(9223372036854775807), false},
		{"octal sign bit", "01000000000000000000000", float64(-9223372036854775808), false},
		{"octal all bits", "01777777777777777777777", -1, false},
		{"octal overflow", "02000000000000000000000", 0, true},
		{"decimal signed maximum", "9223372036854775807", float64(9223372036854775807), false},
		{"decimal beyond signed maximum", "9223372036854775808", float64(9223372036854775808), false},
		{"decimal unsigned maximum", "18446744073709551615", float64(18446744073709549568), false},
		{"decimal negative unsigned maximum", "-18446744073709551615", float64(-18446744073709549568), false},
		{"decimal large mantissa", "9223372036854777855", float64(9223372036854775808), false},
		{"decimal negative large mantissa", "-9223372036854777855", float64(-9223372036854775808), false},
		{"decimal signed minimum", "-9223372036854775808", float64(-9223372036854775808), false},
		{"decimal beyond signed minimum", "-9223372036854775809", float64(-9223372036854775808), false},
		{"decimal Int64 rounds to nearest", "9007199254740995", float64(9007199254740996), false},
		{"decimal negative Int64 rounds to nearest", "-9007199254740995", float64(-9007199254740996), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := "fromJSON('" + tc.source + "')"
			value, err := json.FromJSONValue(tc.source)
			node := syntax.ParseAssignedExpression("${{ " + input + " }}")
			if node == nil {
				t.Fatal("expression did not parse")
			}
			folded, known := evaluate.ConditionConstantValue(node)
			result := evaluate.Evaluate(node, nil)
			want := expressions.Expectation{Value: tc.want}
			if tc.invalid {
				if err == nil || known || result.State != exprvalues.EvaluationInvalid {
					t.Fatalf("accepted overflowing integer: parsed=%v error=%v folded=%v known=%t evaluation=%+v", value, err, folded, known, result)
				}
				want = expressions.Expectation{Failure: expressions.RuntimeFailure, ExceptionType: "Newtonsoft.Json.JsonReaderException"}
			} else if err != nil || value != tc.want || !known || folded != tc.want || result.State != exprvalues.EvaluationKnown || result.Value != tc.want {
				t.Fatalf("parsed=%v error=%v folded=%v known=%t evaluation=%+v, want %v", value, err, folded, known, result, tc.want)
			}
			expressions.CheckOutcomeWithJSONMode(t, input, nil, expressions.LooseJSON, want)
		})
	}
}

func TestFromJSONLargeIntegerFloatBoundariesAgainstRunner(t *testing.T) {
	t.Parallel()
	maximum, _ := new(big.Float).SetFloat64(math.MaxFloat64).Int(nil)
	aboveMaximum := new(big.Int).Add(maximum, big.NewInt(1))
	overflow := new(big.Int).Lsh(big.NewInt(1), 1024)
	for _, tc := range []struct {
		name  string
		value *big.Int
		want  float64
	}{
		{"maximum finite", maximum, math.MaxFloat64},
		{"above maximum finite truncates", aboveMaximum, math.MaxFloat64},
		{"overflow", overflow, math.Inf(1)},
	} {
		for _, negative := range []bool{false, true} {
			name, source, want := tc.name, tc.value.String(), tc.want
			if negative {
				name, source, want = "negative "+name, "-"+source, -want
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				value, err := json.FromJSONValue(source)
				if err != nil || value != want {
					t.Fatalf("parsed=%v error=%v, want %v", value, err, want)
				}
				input := "fromJSON('" + source + "')"
				node := syntax.ParseAssignedExpression("${{ " + input + " }}")
				result := evaluate.Evaluate(node, nil)
				if result.State != exprvalues.EvaluationKnown || result.Value != want {
					t.Fatalf("evaluation=%+v, want %v", result, want)
				}
				// JSON cannot encode infinity. Compare it inside the runner.
				if math.IsInf(want, 0) {
					literal := "Infinity"
					if negative {
						literal = "-Infinity"
					}
					expressions.CheckOutcomeWithJSONMode(t, input+" == fromJSON('"+literal+"')", nil, expressions.LooseJSON, expressions.Expectation{Value: true})
				} else {
					expressions.CheckOutcomeWithJSONMode(t, input, nil, expressions.LooseJSON, expressions.Expectation{Value: want})
				}
			})
		}
	}
}

func TestFromJSONNonDecimalIntegerStrictModeAgainstRunner(t *testing.T) {
	t.Parallel()
	expressions.CheckOutcomeWithJSONMode(t, "fromJSON('0xffffffffffffffff')", nil, expressions.StrictJSON,
		expressions.Expectation{Failure: expressions.RuntimeFailure, ExceptionType: "System.Text.Json.JsonException"})
}
