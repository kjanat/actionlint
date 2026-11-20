package exitcode

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestAnalysisSeverityThreshold(t *testing.T) {
	t.Parallel()
	for _, strict := range []bool{false, true} {
		for _, level := range []severity.Severity{"", severity.Info, severity.Warning, severity.Error} {
			for _, advice := range []bool{false, true} {
				analysis := report.Analysis{Diagnostics: []results.PublishedDiagnostic{{Severity: level, NonBlocking: advice}}}
				got := AnalysisResult(analysis, strict)
				want := ExitSuccessNoProblem
				if !advice && (level == "" || level == severity.Error || strict && level == severity.Warning) {
					want = ExitSuccessProblemFound
				}
				threshold := severity.Error
				if strict {
					threshold = severity.Warning
				}
				if got.ExitCode != want || got.FailureThreshold != threshold || len(got.Diagnostics) != 1 {
					t.Fatalf("strict=%t, severity=%q, advice=%t: exit=%d threshold=%s findings=%d; want exit=%d threshold=%s and one visible finding", strict, level, advice, got.ExitCode, got.FailureThreshold, len(got.Diagnostics), want, threshold)
				}
			}
		}
	}
}
