package typecheck

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

// InferredTypes stays fixed after later checks or context updates and
// identifies nodes by identity in the checked syntax tree.
type InferredTypes struct {
	byNode map[syntax.Node]exprtypes.Type
}

// TypeOf reports false when the checker inferred no type, which differs from any.
func (types InferredTypes) TypeOf(node syntax.Node) (exprtypes.Type, bool) {
	ty, found := types.byNode[node]
	return ty, found
}
