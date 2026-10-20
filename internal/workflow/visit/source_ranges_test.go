package visit_test

import (
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

func TestRuleDiagnosticSourceRange(t *testing.T) {
	rule := workflowvisit.NewBase("test", "")
	rule.ErrorfRange(&workflowlang.Pos{Line: 1, Col: 2}, &workflowlang.Pos{Line: 2, Col: 4}, "multi-line")
	diagnostic := rule.Errs()[0].Canonical([]byte("abc\ndefgh"))
	if diagnostic.Start != (diagmodel.Position{Line: 1, Column: 2}) || diagnostic.End != (diagmodel.Position{Line: 2, Column: 4}) {
		t.Fatal(diagnostic)
	}
}
