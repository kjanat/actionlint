package typecheck

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

// CheckedCalls identifies nodes by identity in the checked syntax tree.
type CheckedCalls struct {
	byNode map[*syntax.FuncCallNode]functions.FunctionCallFacts
}

// CallOf returns a detached record and reports false for calls without a
// function contract, including calls from another syntax tree.
func (calls CheckedCalls) CallOf(node *syntax.FuncCallNode) (functions.FunctionCallFacts, bool) {
	facts, found := calls.byNode[node]
	return facts.Clone(), found
}

func (calls CheckedCalls) HasMatchedSignature(node *syntax.FuncCallNode) bool {
	return calls.byNode[node].SignatureMatched
}
