package typecheck

import (
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

// EvaluationResults supplies a syntax tree and its precomputed value results.
// Neither operation may evaluate expressions. Result returns caller-owned values;
// a missing result differs from a recorded unknown result.
type EvaluationResults interface {
	Expression() exprsyntax.Node
	Result(exprsyntax.Node) (exprvalues.EvaluationResult, bool)
}
