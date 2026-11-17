package typecheck

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func TestSyntaxFailureIsSeparateFromTypeDiagnostics(t *testing.T) {
	t.Parallel()
	_, issue := syntax.NewParser().ParseExpression(syntax.NewLexer("github.[}}"))
	if issue == nil || issue.Message == "" || issue.Position.Line != 1 || issue.Position.Column < 1 {
		t.Fatalf("expected a located syntax failure: %+v", issue)
	}
}
