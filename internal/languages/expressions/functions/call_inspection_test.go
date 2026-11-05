package functions

import (
	"reflect"
	"testing"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func TestInspectKnownCallArguments(t *testing.T) {
	for _, tc := range []struct {
		name, function string
		values         []any
		kind           exprtypes.Kind
		problems       []CallProblem
	}{
		{"missing format replacement", "format", []any{"{1}", true}, exprtypes.KindString, []CallProblem{MissingFormatArgument{Template: "{1}", Index: 1, Count: 1}}},
		{"malformed format placeholder", "format", []any{"{"}, exprtypes.KindString, []CallProblem{InvalidFormatPlaceholder{Template: "{"}}},
		{"unsupported format specifier", "format", []any{"{0:N2}", true}, exprtypes.KindString, []CallProblem{UnsupportedFormatSpecifier{Template: "{0:N2}", Specifier: "N2"}}},
		{"JSON boolean refines return", "fromjson", []any{"true"}, exprtypes.KindBool, nil},
		{"JSON array refines return", "fromjson", []any{"[1,2]"}, exprtypes.KindArray, nil},
		{"JSON negative zero has numeric type", "fromjson", []any{"-0"}, exprtypes.KindNumber, nil},
		{"unsupported JSON constructor has unknown type", "fromjson", []any{"new Date(0)"}, exprtypes.KindAny, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			function := Builtins[tc.function]
			signature := function.Signatures[0]
			arguments := make([]ParameterMatch, len(tc.values))
			for index, value := range tc.values {
				argument := CallArgument{Source: exprtypes.TypeOfJSONValue(value), State: ArgumentKnown, Value: value}
				arguments[index] = MatchParameter(signature.ParameterAt(index, len(arguments)), argument, signature.CompositeStringFallback)
			}
			got := function.InspectArguments(signature, arguments)
			if got.ReturnType.Kind() != tc.kind || !reflect.DeepEqual(got.Problems, tc.problems) {
				t.Fatalf("inspection=%+v, want type=%v problems=%+v", got, tc.kind, tc.problems)
			}
		})
	}
}

func TestEmptyJSONFailureHasZeroOffset(t *testing.T) {
	read, invalid := readFunctionJSON("").(InvalidJSON)
	if !invalid || read.Offset == nil || *read.Offset != 0 {
		t.Fatalf("empty JSON failure = %+v, want offset 0", read)
	}
}

func TestUnsupportedJSONStillReportsMemberCollisions(t *testing.T) {
	function := Builtins["fromjson"]
	signature := function.Signatures[0]
	argument := CallArgument{Source: exprtypes.StringType(), State: ArgumentKnown, Value: `{"A":1,"a":new Date(0)}`}
	inspection := function.InspectArguments(signature, []ParameterMatch{MatchParameter(signature.Params[0], argument, false)})
	if inspection.ReturnType.Kind() != exprtypes.KindAny || len(inspection.Problems) != 1 {
		t.Fatalf("inspection=%+v, want unknown return type and one collision", inspection)
	}
	collision, ok := inspection.Problems[0].(JSONMemberCollision)
	if !ok || collision.First != "A" || collision.Second != "a" || collision.Offset <= 0 {
		t.Fatalf("problem=%+v, want collision between A and a with its JSON offset", inspection.Problems[0])
	}
}
