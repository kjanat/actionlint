package matrix

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

func TestMatrixFilterTypeMismatchUsesSemanticValues(t *testing.T) {
	for _, tc := range []struct {
		name       string
		axis, filt value.Value
		want       bool
	}{
		{"same string", value.YAMLScalar("1", "!!str", value.Origin{}), value.YAMLScalar("1", "!!str", value.Origin{}), false},
		{"string number", value.YAMLScalar("1", "!!str", value.Origin{}), value.YAMLScalar("1", "!!int", value.Origin{}), true},
		{"dynamic string", value.Template("${{ inputs.value }}", value.Origin{}), value.YAMLScalar("", "!!str", value.Origin{}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := typeMismatch(tc.axis, tc.filt); got != tc.want {
				t.Fatalf("mismatch=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolutionRetainsExpressionOrigin(t *testing.T) {
	got := value.Template("${{ inputs.value }}", value.Origin{})
	if got.State() != value.Dynamic || !strings.Contains(got.Source(), "inputs.value") {
		t.Fatalf("dynamic value lost expression source: %#v", got)
	}
}
