// Package python replaces workflow expressions with values that fit Python
// syntax and preserve line and column positions.
package python

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"

	"actionlint.kjanat.dev/v2/internal/externallinters/preparation"
)

const ID = "kjanat:actionlint/python-preparation@0.1.0"

// ExpressionEnd receives the input after the opening ${{ and returns the byte length through the closing delimiter.
type ExpressionEnd func(string) (int, bool)

func TemplateEnd(src string) (int, bool) {
	lexer := syntax.NewLexer(src)
	_, err := syntax.NewParser().ParseExpression(lexer)
	return lexer.Offset(), err == nil
}

// Sanitize callers must skip the script when the boolean result is false.
func Sanitize(src string, end ExpressionEnd) (string, bool, error) {
	return sanitize(src, end, nil)
}

func Mask(src string, end ExpressionEnd) (string, *Masks, bool, error) {
	masks := &Masks{identifiers: make(map[sourcetext.Position]int), parentheses: make(map[sourcetext.Position]sourcetext.Position), quoted: make(map[sourcetext.Position]string)}
	source, valid, err := sanitize(src, end, masks)
	return source, masks, valid, err
}

func Script(text string) (preparation.Prepared, error) {
	source, masks, valid, err := Mask(text, TemplateEnd)
	if err != nil {
		return preparation.Prepared{}, err
	}
	if !valid {
		return preparation.Prepared{Skip: "its templates cannot be masked without changing Python syntax"}, nil
	}
	prepared := preparation.Prepared{Source: source, AcceptDiagnostic: masks.Accept}
	if strings.Contains(text, "${{") {
		prepared.TemplateContract = scriptengine.TemplateEffectsContract
	}
	return prepared, nil
}
