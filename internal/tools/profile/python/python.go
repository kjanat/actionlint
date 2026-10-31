// Package python is the Python preparation profile. Its token-aware sanitizer
// replaces each template with an opaque value that keeps Python's syntax,
// rows and columns, records the masks so findings they manufacture can be
// filtered, and skips scripts whose templates cannot be masked that way. The
// profile also selects Python step shells and infers their target version.
package python

import (
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/tools/profile"
)

// ID names this profile in tool provider manifests.
const ID = "kjanat:actionlint/python-preparation@0.1.0"

// ExpressionEnd returns the byte length of a valid expression, including its
// closing delimiter. The input starts immediately after the opening ${{.
type ExpressionEnd func(string) (int, bool)

// TemplateEnd is the ExpressionEnd of the workflow expression parser.
func TemplateEnd(src string) (int, bool) {
	lexer := expression.NewLexer(src)
	_, err := expression.NewParser().Parse(lexer)
	return lexer.Offset(), err == nil
}

// Sanitize masks templates with independent opaque values, preserving Unicode
// columns and line breaks. Scripts whose templates cannot preserve Python
// syntax and positions are not valid and must be skipped.
func Sanitize(src string, end ExpressionEnd) (string, bool, error) {
	return sanitize(src, end, nil)
}

// Mask sanitizes src like Sanitize and returns the masks that filter the
// findings its opaque values manufacture.
func Mask(src string, end ExpressionEnd) (string, *Masks, bool, error) {
	masks := &Masks{identifiers: make(map[analyzer.ScriptPosition]int), parentheses: make(map[analyzer.ScriptPosition]analyzer.ScriptPosition), quoted: make(map[analyzer.ScriptPosition]string)}
	source, valid, err := sanitize(src, end, masks)
	return source, masks, valid, err
}

// Script prepares a Python snippet for a tool: masked source, the
// template-effects contract when it holds templates and the masks' filter.
// A script whose templates cannot be masked is skipped.
func Script(text string) (profile.Prepared, error) {
	source, masks, valid, err := Mask(text, TemplateEnd)
	if err != nil {
		return profile.Prepared{}, err
	}
	if !valid {
		return profile.Prepared{Skip: "its templates cannot be masked without changing Python syntax"}, nil
	}
	prepared := profile.Prepared{Source: source, AcceptDiagnostic: masks.Accept}
	if strings.Contains(text, "${{") {
		prepared.TemplateContract = analyzer.TemplateEffectsContract
	}
	return prepared, nil
}
