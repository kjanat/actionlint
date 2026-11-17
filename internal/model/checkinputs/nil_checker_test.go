package checkinputs

import (
	"context"
	"testing"

	"actionlint.kjanat.dev/v2/internal/results"
)

type pointerAnalyzer struct{}

func (*pointerAnalyzer) Descriptor() Descriptor {
	return Descriptor{Name: "pointer"}
}

func (*pointerAnalyzer) CheckDocument(context.Context, AnalyzerInput) ([]results.Diagnostic, error) {
	return nil, nil
}

func TestIsNilChecker(t *testing.T) {
	t.Parallel()
	var typed *pointerAnalyzer
	for _, tc := range []struct {
		name  string
		value Checker
		want  bool
	}{
		{"nil interface", nil, true},
		{"typed nil pointer", typed, true},
		{"value", &pointerAnalyzer{}, false},
	} {
		if got := IsNilChecker(tc.value); got != tc.want {
			t.Errorf("%s: IsNilChecker = %v, want %v", tc.name, got, tc.want)
		}
	}
}
