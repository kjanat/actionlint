package typecheck

import (
	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

// CheckedCalls provides function contracts and argument conversions from one check.
// Nodes are identified by their identity in the checked syntax tree.
type CheckedCalls struct {
	byNode map[*exprsyntax.FuncCallNode]exprfunctions.FunctionCallFacts
}

// CallOf returns a detached call record. The second result is false when no
// function contract was found, including calls from another syntax tree.
func (calls CheckedCalls) CallOf(node *exprsyntax.FuncCallNode) (exprfunctions.FunctionCallFacts, bool) {
	facts, found := calls.byNode[node]
	return facts.Clone(), found
}
