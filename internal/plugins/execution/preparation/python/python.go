// Package python replaces workflow expressions with values that fit Python
// syntax and preserve line and column positions. It records the replacements
// to filter diagnostics they cause. If a replacement cannot satisfy these
// constraints, the script is skipped. The package also identifies Python step
// shells and determines their target version.
package python

import (
	"strings"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"

	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation"
)

// ID selects this Python script processing in provider manifests.
const ID = "kjanat:actionlint/python-preparation@0.1.0"

// ExpressionEnd returns the byte length of a valid expression, including its
// closing delimiter. The input starts immediately after the opening ${{.
type ExpressionEnd func(string) (int, bool)

// TemplateEnd is the ExpressionEnd of the workflow expression parser.
func TemplateEnd(src string) (int, bool) {
	lexer := exprsyntax.NewLexer(src)
	_, err := exprsyntax.NewParser().Parse(lexer)
	return lexer.Offset(), err == nil
}

// Sanitize masks templates with independent opaque values, preserving Unicode
// columns and line breaks. The boolean result reports whether masking succeeded.
// Callers must skip the script when it is false. Ruff checks the resulting Python syntax.
func Sanitize(src string, end ExpressionEnd) (string, bool, error) {
	return sanitize(src, end, nil)
}

// Mask sanitizes src like Sanitize and returns masks that filter findings caused
// by the replacement values.
func Mask(src string, end ExpressionEnd) (string, *Masks, bool, error) {
	masks := &Masks{identifiers: make(map[checkinputs.ScriptPosition]int), parentheses: make(map[checkinputs.ScriptPosition]checkinputs.ScriptPosition), quoted: make(map[checkinputs.ScriptPosition]string)}
	source, valid, err := sanitize(src, end, masks)
	return source, masks, valid, err
}

// Script prepares a Python snippet for a tool: masked source, the
// template-effects contract when it holds templates and the masks' filter.
// A script whose templates cannot be masked is skipped.
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
		prepared.TemplateContract = checkinputs.TemplateEffectsContract
	}
	return prepared, nil
}
