package workflowvisit_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
)

func TestRuleDiagnosticSourceRange(t *testing.T) {
	t.Parallel()
	rule := workflowvisit.NewBase("test", "")
	rule.ErrorfRange(&sourcetext.Position{Line: 1, Column: 2}, &sourcetext.Position{Line: 2, Column: 4}, "multi-line")
	diagnostic := rule.Errs()[0].Publish([]byte("abc\ndefgh")).Diagnostic
	if diagnostic.Start != (sourcetext.Position{Line: 1, Column: 2}) || diagnostic.End != (sourcetext.Position{Line: 2, Column: 4}) {
		t.Fatal(diagnostic)
	}
}
