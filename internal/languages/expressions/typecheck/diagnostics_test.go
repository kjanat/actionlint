package typecheck

import (
	"testing"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func TestSyntaxFailureIsSeparateFromTypeDiagnostics(t *testing.T) {
	t.Parallel()
	_, issue := exprsyntax.NewParser().Parse(exprsyntax.NewLexer("github.[}}"))
	if issue == nil || issue.Message == "" || issue.Line != 1 || issue.Column < 1 {
		t.Fatalf("expected a located syntax failure: %+v", issue)
	}
}
