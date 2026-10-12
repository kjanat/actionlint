package actionlint

import (
	"strings"
	"testing"
)

func TestConditionSourceFullConsumption(t *testing.T) {
	for _, source := range []string{
		"${{ 'github.ref }} ignored' }}",
		"${{ 'false }}' }}",
		"github.ref }} ignored",
	} {
		t.Run(source, func(t *testing.T) {
			expr, err, _ := parseConditionExpression(source)
			if err == nil || expr != nil {
				t.Fatalf("accepted incomplete condition: expr=%v err=%v", expr, err)
			}
			if !strings.Contains(err.Message, "unexpected") {
				t.Fatal(err)
			}
		})
	}
}

func TestMatrixMissingAndUnknownFilterTypes(t *testing.T) {
	for _, tc := range []struct {
		name          string
		value, filter any
		want          bool
	}{
		{"missing property string", map[string]any{}, map[string]any{"missing": ""}, true},
		{"missing property null", map[string]any{}, map[string]any{"missing": nil}, false},
		{"missing property number", map[string]any{}, map[string]any{"missing": float64(0)}, true},
		{"missing array string", []any{}, []any{""}, true},
		{"missing array null", []any{}, []any{nil}, false},
		{"array property index", []any{""}, map[string]any{"0": ""}, false},
		{"object array index", map[string]any{"0": ""}, []any{""}, false},
		{"nested missing string", map[string]any{}, map[string]any{"missing": map[string]any{"child": ""}}, true},
		{"dynamic string", "${{ inputs.value }}", "", false},
		{"dynamic nested string", "${{ inputs.value }}", map[string]any{"child": ""}, false},
		{"dynamic nested number", "${{ inputs.value }}", map[string]any{"child": float64(0)}, true},
		{"present null string", nil, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matrixFilterTypeMismatch(matrixLiteralValue(tc.value), matrixLiteralValue(tc.filter), true, true)
			if got != tc.want {
				t.Fatalf("mismatch=%v, want %v", got, tc.want)
			}
		})
	}
}
