package typecheck

import (
	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

// validateCallDeclarations resolves every call before branch refinement or
// evaluation. A branch that cannot execute still needs valid function syntax.
func (sema *Checker) validateCallDeclarations(expr exprsyntax.Node) {
	sema.callSignatures = make(map[*exprsyntax.FuncCallNode][]*exprfunctions.FuncSignature)
	exprsyntax.Visit(expr, func(node, _ exprsyntax.Node, entering bool) {
		call, ok := node.(*exprsyntax.FuncCallNode)
		if !entering || !ok {
			return
		}
		signatures := sema.resolveFunction(call)
		sema.callSignatures[call] = signatures
		if len(signatures) == 0 {
			return
		}
		sema.checkFunctionAvailability(call)
		var failures []*Error
		for _, signature := range signatures {
			err := checkCallArity(call, signature)
			if err == nil {
				return
			}
			failures = append(failures, err)
		}
		for _, err := range failures {
			sema.validationError(err)
		}
	})
}
