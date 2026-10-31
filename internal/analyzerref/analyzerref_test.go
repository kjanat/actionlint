package analyzerref

import (
	"context"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
)

type pointerAnalyzer struct{}

func (*pointerAnalyzer) Descriptor() analyzer.Descriptor { return analyzer.Descriptor{Name: "pointer"} }

func (*pointerAnalyzer) Analyze(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
	return nil, nil
}

func TestIsNil(t *testing.T) {
	var typed *pointerAnalyzer
	for _, tc := range []struct {
		name  string
		value analyzer.Analyzer
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
