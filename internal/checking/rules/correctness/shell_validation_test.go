package correctness

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
	"actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

func TestCheckShellConvertsKnownScalar(t *testing.T) {
	t.Parallel()
	origin := value.Origin{Span: value.Span{Start: sourcetext.Position{Line: 6, Column: 12}}}
	shell := execution.DefaultShell(value.Template(`${{ fromJSON('{"shell":42}') }}`, origin))
	findings := checkShell(shell, runners.MacOrLinux)
	if shell.Kind() != value.Number || len(findings) != 1 || !strings.Contains(findings[0].Message, `shell name "42" is invalid`) || findings[0].Start != origin.Span.Start {
		t.Fatalf("converted scalar validation lost type or location: shell=%v, findings=%v", shell.Kind(), findings)
	}
}
