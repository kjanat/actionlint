package typecheck

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprvalues"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

// EvaluationResults must not evaluate expressions. Result returns caller-owned values;
// a missing result differs from a recorded unknown result.
type EvaluationResults interface {
	Expression() syntax.Node
	Result(syntax.Node) (exprvalues.EvaluationResult, bool)
}
