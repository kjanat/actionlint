package ruff_test

import (
	"strings"
	"testing"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/tools/ruff"
)

func TestSanitizePythonScript(t *testing.T) {
	for _, source := range []string{"print(${{ github.sha }})", "print('${{ '}}' }}')", "print(${{\n github.sha\n}})", "print(${{ 'é' }}, missing)"} {
		got, valid, err := ruff.Sanitize(source, testExpressionEnd)
		if err != nil {
			t.Fatal(err)
		}
		if !valid || strings.Contains(got, "${{") || len([]rune(got)) != len([]rune(source)) {
			t.Fatalf("%q -> %q, %v", source, got, valid)
		}
		for i, r := range []rune(source) {
			if (r == '\n' || r == '\r') && []rune(got)[i] != r {
				t.Fatal("line break moved")
			}
		}
	}
	if _, valid, _ := ruff.Sanitize("print(${{ missing)", testExpressionEnd); valid {
		t.Fatal("accepted malformed template")
	}
}
func testExpressionEnd(src string) (int, bool) {
	lexer := exprlang.NewLexer(src)
	_, err := exprlang.NewParser().Parse(lexer)
	return lexer.Offset(), err == nil
}
