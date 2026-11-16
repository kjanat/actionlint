package value

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func TestTypeWithFallbackSeparatesValuesFromEvaluationState(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		item Value
		want types.TypeKind
	}{
		{"null", FromJSON(nil, Origin{}), types.KindNull},
		{"boolean", FromJSON(true, Origin{}), types.KindBool},
		{"number", FromJSON(1, Origin{}), types.KindNumber},
		{"string", FromJSON("value", Origin{}), types.KindString},
		{"dynamic", Template("${{ inputs.value }}", Origin{}), types.KindNumber},
		{"invalid", Template("${{ true + }}", Origin{}), types.KindNumber},
		{"missing", Value{}, types.KindNumber},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state, known := tc.item.State(), tc.item.Known()
			if ty := tc.item.TypeWithFallback(types.NumberType()); ty.Kind() != tc.want {
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
	fallback := types.NewStrictObjectType(map[string]types.Type{
		"known": types.BoolType(), "unknown": types.NumberType(),
		"array": types.NewArrayType(types.BoolType()).Type(), "absent": types.StringType(),
	})
	object, ok := item.TypeWithFallback(fallback.Type()).Object()
	if !ok || !object.IsStrict() || object.Len() != 4 {
		t.Fatal(object)
	}
	for name, kind := range map[string]types.TypeKind{"known": types.KindString, "unknown": types.KindNumber, "untyped": types.KindAny} {
		if ty, found := object.Property(name); !found || ty.Kind() != kind {
			t.Fatalf("%s=%v, want %v", name, ty, kind)
		}
	}
	arrayType, _ := object.Property("array")
	array, ok := arrayType.Array()
	if !ok || array.Elem().Kind() != types.KindBool {
		t.Fatal(arrayType)
	}
	for _, tc := range []struct {
		item Value
		kind types.TypeKind
	}{
		{Seq(Origin{}), types.KindAny},
		{Seq(Origin{}, FromJSON(1, Origin{}), FromJSON(2, Origin{})), types.KindNumber},
		{Seq(Origin{}, FromJSON(1, Origin{}), FromJSON("text", Origin{})), types.KindAny},
	} {
		array, ok := tc.item.TypeWithFallback(types.AnyType()).Array()
		if !ok || array.Elem().Kind() != tc.kind {
			t.Fatalf("array=%v, want element kind %v", array, tc.kind)
		}
	}
}
