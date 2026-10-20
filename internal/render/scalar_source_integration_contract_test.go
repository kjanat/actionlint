package render_test

import (
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/render"
)

func TestDiagnosticLongSourceLine(t *testing.T) {
	line := strings.Repeat("x", 70*1024)
	for _, raw := range []bool{false, true} {
		finding := &findingmodel.Finding{Start: diagmodel.Position{Line: 2, Column: 1}, RawScriptSource: raw}
		if got, ok := render.SourceLine([]byte("first\n"+line+"\n"), finding.Start.Line, finding.RawScriptSource); !ok || got != line {
			t.Fatalf("raw=%v: long source line missing or truncated", raw)
		}
	}
}
