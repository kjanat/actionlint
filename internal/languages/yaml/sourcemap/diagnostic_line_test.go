package sourcemap_test

import (
	"strings"
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

func TestDiagnosticLongSourceLine(t *testing.T) {
	t.Parallel()
	line := strings.Repeat("x", 70*1024)
	for _, raw := range []bool{false, true} {
		finding := &findingmodel.Finding{Start: sourcepos.Position{Line: 2, Column: 1}, RawScriptSource: raw}
		if got, ok := sourcemap.DiagnosticLine([]byte("first\n"+line+"\n"), finding.Start.Line, finding.RawScriptSource); !ok || got != line {
			t.Fatalf("raw=%v: long source line missing or truncated", raw)
		}
	}
}
