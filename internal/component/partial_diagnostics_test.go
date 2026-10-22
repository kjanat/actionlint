package component

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/diagnostic"
	"github.com/partite-ai/wacogo"
)

func TestLiftAnalysisPreservesValidSiblings(t *testing.T) {
	values := wacogo.NewValListOf[wacogo.Val](
		mixedDiagnostic("before", diagnostic.Position{Line: 1, Column: 1}, diagnostic.Position{Line: 1, Column: 2}),
		mixedDiagnostic("zero", diagnostic.Position{}, diagnostic.Position{Line: 2, Column: 2}),
		mixedDiagnostic("between", diagnostic.Position{Line: 3, Column: 1}, diagnostic.Position{Line: 3, Column: 2}),
		mixedDiagnostic("reversed", diagnostic.Position{Line: 4, Column: 8}, diagnostic.Position{Line: 4, Column: 2}),
		mixedDiagnostic("after", diagnostic.Position{Line: 5, Column: 1}, diagnostic.Position{Line: 5, Column: 2}),
	)
	findings, err := liftAnalysis(wacogo.ValResultOk(values))
	if len(findings) != 3 {
		t.Fatalf("valid siblings lost: findings=%+v err=%v", findings, err)
	}
	for i, want := range []string{"before", "between", "after"} {
		if findings[i].Message != want {
			t.Errorf("finding %d=%q, want %q", i, findings[i].Message, want)
		}
	}
	for _, want := range []string{"diagnostic 1", "one-based", "diagnostic 3", "end precedes start"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("missing validation failure %q: %v", want, err)
		}
	}
}

func TestLiftAnalysisRejectsMalformedEnvelope(t *testing.T) {
	for name, value := range map[string]wacogo.Val{
		"wrong outcome":    wacogo.ValString("not a result"),
		"wrong list":       wacogo.ValResultOk(wacogo.ValString("not a list")),
		"wrong error":      wacogo.ValResultErr(wacogo.ValU32(1)),
		"explicit failure": wacogo.ValResultErr(wacogo.ValString("analysis failed")),
	} {
		t.Run(name, func(t *testing.T) {
			findings, err := liftAnalysis(value)
			if findings != nil || err == nil {
				t.Fatalf("malformed envelope accepted: findings=%+v err=%v", findings, err)
			}
		})
	}
}

func mixedDiagnostic(message string, start, end diagnostic.Position) *wacogo.ValRecord {
	return wacogo.NewValRecord(
		field("rule", wacogo.ValString("example")), field("code", wacogo.ValString("EX001")),
		field("severity", wacogo.ValString("warning")), field("message", wacogo.ValString(message)),
		field("path", wacogo.ValString("workflow.yml")),
		field("start", lowerPosition(start)), field("end", lowerPosition(end)),
		field("fixes", wacogo.NewValListOf[wacogo.Val]()),
	)
}
