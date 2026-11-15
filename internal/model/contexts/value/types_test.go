package value

import (
	"testing"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func TestTypeWithFallbackSeparatesValuesFromEvaluationState(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		item Value
		want exprtypes.Kind
	}{
		{"null", FromJSON(nil, Origin{}), exprtypes.KindNull},
		{"boolean", FromJSON(true, Origin{}), exprtypes.KindBool},
		{"number", FromJSON(1, Origin{}), exprtypes.KindNumber},
		{"string", FromJSON("value", Origin{}), exprtypes.KindString},
		{"dynamic", Template("${{ inputs.value }}", Origin{}), exprtypes.KindNumber},
		{"invalid", Template("${{ true + }}", Origin{}), exprtypes.KindNumber},
		{"missing", Value{}, exprtypes.KindNumber},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state, known := tc.item.State(), tc.item.Known()
			if ty := tc.item.TypeWithFallback(exprtypes.NumberType()); ty.Kind() != tc.want {
				t.Fatalf("type=%v, want kind %v", ty, tc.want)
			}
			if tc.item.State() != state || tc.item.Known() != known {
				t.Fatal("type projection changed evaluation state")
			}
		})
	}
}

func TestTypeWithFallbackUsesOnlyCorrespondingUnknownParts(t *testing.T) {
	t.Parallel()
	unknown := Template("${{ inputs.value }}", Origin{})
	item := Map(Origin{},
		Prop{Name: "known", Value: FromJSON("text", Origin{})},
		Prop{Name: "unknown", Value: unknown},
		Prop{Name: "untyped", Value: unknown},
		Prop{Name: "array", Value: Seq(Origin{}, unknown)},
	)
	fallback := exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
		"known": exprtypes.BoolType(), "unknown": exprtypes.NumberType(),
		"array": exprtypes.NewArrayType(exprtypes.BoolType()).Type(), "absent": exprtypes.StringType(),
	})
	object, ok := item.TypeWithFallback(fallback.Type()).Object()
	if !ok || !object.IsStrict() || object.Len() != 4 {
		t.Fatal(object)
	}
	for name, kind := range map[string]exprtypes.Kind{"known": exprtypes.KindString, "unknown": exprtypes.KindNumber, "untyped": exprtypes.KindAny} {
		if ty, found := object.Property(name); !found || ty.Kind() != kind {
			t.Fatalf("%s=%v, want %v", name, ty, kind)
		}
	}
	arrayType, _ := object.Property("array")
	array, ok := arrayType.Array()
	if !ok || array.Elem().Kind() != exprtypes.KindBool {
		t.Fatal(arrayType)
	}
	for _, tc := range []struct {
		item Value
		kind exprtypes.Kind
	}{
		{Seq(Origin{}), exprtypes.KindAny},
		{Seq(Origin{}, FromJSON(1, Origin{}), FromJSON(2, Origin{})), exprtypes.KindNumber},
		{Seq(Origin{}, FromJSON(1, Origin{}), FromJSON("text", Origin{})), exprtypes.KindAny},
	} {
		array, ok := tc.item.TypeWithFallback(exprtypes.AnyType()).Array()
		if !ok || array.Elem().Kind() != tc.kind {
			t.Fatalf("array=%v, want element kind %v", array, tc.kind)
		}
	}
}
