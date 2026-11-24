package typecheck

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprvalues"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

// EvaluationResults implementations return clones of recorded results. Result reports false for a node without a recorded result and true for a recorded unknown result.
type EvaluationResults interface {
	Expression() syntax.Node
	Result(syntax.Node) (exprvalues.EvaluationResult, bool)
}
