package workflowvisit_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
)

func TestRuleDiagnosticSourceRange(t *testing.T) {
	t.Parallel()
	rule := workflowvisit.NewBase("test", "")
	rule.ErrorfRange(&sourcepos.Position{Line: 1, Column: 2}, &sourcepos.Position{Line: 2, Column: 4}, "multi-line")
	diagnostic := rule.Errs()[0].Publish([]byte("abc\ndefgh")).Diagnostic
	if diagnostic.Start != (sourcepos.Position{Line: 1, Column: 2}) || diagnostic.End != (sourcepos.Position{Line: 2, Column: 4}) {
		t.Fatal(diagnostic)
	}
}
