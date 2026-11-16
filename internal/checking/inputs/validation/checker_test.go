package validation

import (
	"context"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/results"
)

type pointerAnalyzer struct{}

func (*pointerAnalyzer) Descriptor() inputs.Descriptor {
	return inputs.Descriptor{Name: "pointer"}
}

func (*pointerAnalyzer) CheckDocument(context.Context, inputs.AnalyzerInput) ([]results.Diagnostic, error) {
	return nil, nil
}

func TestIsNil(t *testing.T) {
	t.Parallel()
	var typed *pointerAnalyzer
	for _, tc := range []struct {
		name  string
		value inputs.Checker
		want  bool
	}{
		{"nil interface", nil, true},
		{"typed nil pointer", typed, true},
		{"value", &pointerAnalyzer{}, false},
	} {
		if got := IsNil(tc.value); got != tc.want {
			t.Errorf("%s: IsNil = %v, want %v", tc.name, got, tc.want)
		}
	}
}
