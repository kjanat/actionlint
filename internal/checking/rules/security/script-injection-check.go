package security

import (
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
)

// ScriptInjection reports untrusted values and secrets inserted into script text.
// Semantic errors exclude invalid secret selectors without hiding findings in other branches.
func ScriptInjection(expr exprsyntax.Node, semanticErrors []*exprlang.Error) []*exprlang.Error {
	findings := checkUntrustedInputs(expr)
	return append(findings, checkSecretInterpolations(expr, semanticErrors)...)
}
