package sourcemap_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

func TestDiagnosticLongSourceLine(t *testing.T) {
	t.Parallel()
	line := strings.Repeat("x", 70*1024)
	for _, raw := range []bool{false, true} {
		finding := &finding.Finding{Start: sourcetext.Position{Line: 2, Column: 1}, Source: results.DiagnosticSource{RawScript: raw}}
		if got, ok := sourcemap.DiagnosticLine([]byte("first\n"+line+"\n"), finding.Start.Line, finding.Source.RawScript); !ok || got != line {
			t.Fatalf("raw=%v: long source line missing or truncated", raw)
		}
	}
}
