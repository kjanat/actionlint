package typecheck

import (
	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

// CheckedCalls provides function contracts and argument conversions from one check.
// Nodes are identified by their identity in the checked syntax tree.
type CheckedCalls struct {
	byNode map[*syntax.FuncCallNode]exprfunctions.FunctionCallFacts
}

// CallOf returns a detached call record. The second result is false when no
// function contract was found, including calls from another syntax tree.
func (calls CheckedCalls) CallOf(node *syntax.FuncCallNode) (exprfunctions.FunctionCallFacts, bool) {
	facts, found := calls.byNode[node]
	return facts.Clone(), found
}

// HasMatchedSignature reports whether the checker selected a signature for this call.
// Unchecked calls and calls rejected by every signature return false.
func (calls CheckedCalls) HasMatchedSignature(node *syntax.FuncCallNode) bool {
	return calls.byNode[node].SignatureMatched
}
