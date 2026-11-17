package results

import (
	"context"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
)

type pointerAnalyzer struct{}

func (*pointerAnalyzer) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "pointer"}
}

func (*pointerAnalyzer) AnalyzeDocument(context.Context, checkinputs.AnalyzerInput) ([]Diagnostic, error) {
	return nil, nil
}

func TestIsNilAnalyzer(t *testing.T) {
	t.Parallel()
	var typed *pointerAnalyzer
	for _, tc := range []struct {
		name  string
		value Analyzer
		want  bool
	}{
		{"nil interface", nil, true},
		{"typed nil pointer", typed, true},
		{"value", &pointerAnalyzer{}, false},
	} {
		if got := IsNilAnalyzer(tc.value); got != tc.want {
			t.Errorf("%s: IsNilAnalyzer = %v, want %v", tc.name, got, tc.want)
		}
	}
}
