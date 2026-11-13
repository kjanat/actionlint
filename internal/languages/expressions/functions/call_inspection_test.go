package functions

import (
	"reflect"
	"testing"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func TestInspectKnownCallArguments(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, function string
		values         []any
		kind           exprtypes.Kind
		problems       []CallProblemReport
	}{
		{"missing format replacement", "format", []any{"{1}", true}, exprtypes.KindString, []CallProblemReport{{Problem: MissingFormatArgument{Template: "{1}", Index: 1, Count: 1}, Demand: DemandRequired}}},
		{"malformed format placeholder", "format", []any{"{"}, exprtypes.KindString, []CallProblemReport{{Problem: InvalidFormatPlaceholder{Template: "{"}, Demand: DemandRequired}}},
		{"unsupported format specifier", "format", []any{"{0:N2}", true}, exprtypes.KindString, []CallProblemReport{{Problem: UnsupportedFormatSpecifier{Template: "{0:N2}", Specifier: "N2"}, Demand: DemandRequired}}},
		{"JSON boolean refines return", "fromjson", []any{"true"}, exprtypes.KindBool, nil},
		{"JSON array refines return", "fromjson", []any{"[1,2]"}, exprtypes.KindArray, nil},
		{"JSON negative zero has numeric type", "fromjson", []any{"-0"}, exprtypes.KindNumber, nil},
		{"unsupported JSON constructor has unknown type", "fromjson", []any{"new Date(0)"}, exprtypes.KindAny, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
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
	t.Parallel()
	read, invalid := readFunctionJSON("").(InvalidJSON)
	if !invalid || read.Offset == nil || *read.Offset != 0 {
		t.Fatalf("empty JSON failure = %+v, want offset 0", read)
	}
}

func TestUnsupportedJSONStillReportsMemberCollisions(t *testing.T) {
	t.Parallel()
	function := Builtins["fromjson"]
	signature := function.Signatures[0]
	argument := CallArgument{Source: exprtypes.StringType(), State: ArgumentKnown, Value: `{"A":1,"a":new Date(0)}`}
	inspection := function.InspectArguments(signature, []ParameterMatch{MatchParameter(signature.Params[0], argument, false)})
	if inspection.ReturnType.Kind() != exprtypes.KindAny || len(inspection.Problems) != 1 {
		t.Fatalf("inspection=%+v, want unknown return type and one collision", inspection)
	}
	collision, ok := inspection.Problems[0].Problem.(JSONMemberCollision)
	if !ok || collision.First != "A" || collision.Second != "a" || collision.Offset <= 0 {
		t.Fatalf("problem=%+v, want collision between A and a with its JSON offset", inspection.Problems[0])
	}
}

func TestCaseInspectionPreservesPredicateDemand(t *testing.T) {
	t.Parallel()
	boolean := CallArgument{Source: exprtypes.BoolType(), Demand: DemandRequired}
	value := CallArgument{Source: exprtypes.StringType(), Demand: DemandSkipped}
	for _, tc := range []struct {
		name      string
		arguments []CallArgument
		problems  []CallProblemReport
	}{
		{"required nonboolean", []CallArgument{
			{Source: exprtypes.AnyType(), State: ArgumentKnown, Value: float64(1), Demand: DemandRequired}, value, value,
		}, []CallProblemReport{{Problem: InvalidCasePredicate{Index: 0, Actual: exprtypes.NumberType()}, Demand: DemandRequired}}},
		{"skipped nonboolean", []CallArgument{
			{Source: exprtypes.NumberType(), Demand: DemandSkipped}, value, value,
		}, nil},
		{"conditional later predicate", []CallArgument{
			boolean, value, {Source: exprtypes.StringType(), State: ArgumentKnown, Value: "text", Demand: DemandConditional}, value, value,
		}, []CallProblemReport{{Problem: InvalidCasePredicate{Index: 2, Actual: exprtypes.StringType()}, Demand: DemandConditional}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			function := Builtins["case"]
			signature := function.Signatures[0]
			arguments := make([]ParameterMatch, len(tc.arguments))
			for index, argument := range tc.arguments {
				arguments[index] = MatchParameter(signature.ParameterAt(index, len(arguments)), argument, false)
			}
			got := function.InspectArguments(signature, arguments)
			if !reflect.DeepEqual(got.Problems, tc.problems) {
				t.Fatalf("problems=%+v, want %+v", got.Problems, tc.problems)
			}
		})
	}
}
