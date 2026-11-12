package visit_test

import (
	"testing"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestRuleDiagnosticSourceRange(t *testing.T) {
	t.Parallel()
	rule := workflowvisit.NewBase("test", "")
	rule.ErrorfRange(&workflowlang.Pos{Line: 1, Col: 2}, &workflowlang.Pos{Line: 2, Col: 4}, "multi-line")
	diagnostic := rule.Errs()[0].Canonical([]byte("abc\ndefgh"))
	if diagnostic.Start != (sourcepos.Position{Line: 1, Column: 2}) || diagnostic.End != (sourcepos.Position{Line: 2, Column: 4}) {
		t.Fatal(diagnostic)
	}
}
