package names

import (
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

type declaredKind uint8

const (
	// unknownName: the name could not be determined.
	unknownName declaredKind = iota
	// unnamed: the workflow has no or an empty top-level name; GitHub shows its path.
	unnamed
	// literalName: plain text that GitHub shows as written.
	literalName
	// evaluatedName: a ${{ }} literal expression; filters match its value.
	evaluatedName
)

// Declared is the top-level name one workflow file declares. The zero value
// is an unknown name.
type Declared struct {
	kind  declaredKind
	value string
}

// Unknown is a name that could not be determined, such as one containing
// an expression or a workflow that does not parse.
func Unknown() Declared { return Declared{} }

// Declare classifies a parsed workflow's top-level name. Nil or an empty
// name means the workflow declares no name.
func Declare(name *workflowlang.String) Declared {
	if name == nil || name.Value == "" {
		return Declared{kind: unnamed}
	}
	if literal := exprsyntax.LiteralExpressionValue(name.Value); literal != nil {
		if *literal == "" {
			return Declared{kind: unnamed}
		}
		return Declared{kind: evaluatedName, value: *literal}
	}
	if name.ContainsExpression() {
		return Unknown()
	}
	return Declared{kind: literalName, value: name.Value}
}

// Known reports whether the name, or its absence, could be determined.
func (d Declared) Known() bool { return d.kind != unknownName }

// Explicit returns the declared name that workflow_run filters match. It
// returns false for an unnamed or unknown workflow.
func (d Declared) Explicit() (string, bool) {
	return d.value, d.kind == literalName || d.kind == evaluatedName
}

// Literal returns a name written as plain text, which GitHub shows exactly
// as written. It returns false for names written as expressions.
func (d Declared) Literal() (string, bool) {
	return d.value, d.kind == literalName
}
