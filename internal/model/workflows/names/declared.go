package names

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

type declaredKind uint8

const (
	unknownName declaredKind = iota
	// GitHub shows the path of a workflow with no or an empty top-level name.
	unnamed
	literalName
	// workflow_run filters match the value of a ${{ }} literal expression.
	evaluatedName
)

type Declared struct {
	kind  declaredKind
	value string
}

func Unknown() Declared { return Declared{} }

func Declare(name *definition.String) Declared {
	if name == nil || name.Value == "" {
		return Declared{kind: unnamed}
	}
	if literal := syntax.LiteralExpressionValue(name.Value); literal != nil {
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

func (d Declared) Known() bool { return d.kind != unknownName }

// Explicit returns the declared name that workflow_run filters match.
func (d Declared) Explicit() (string, bool) {
	return d.value, d.kind == literalName || d.kind == evaluatedName
}

func (d Declared) Literal() (string, bool) {
	return d.value, d.kind == literalName
}
