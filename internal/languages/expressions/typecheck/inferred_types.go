package typecheck

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

// InferredTypes provides the types inferred during one expression check.
// A later check or context update does not change this result.
// Nodes are identified by their identity in the checked syntax tree.
type InferredTypes struct {
	byNode map[syntax.Node]exprtypes.Type
}

// TypeOf returns a node's inferred type. The second result is false when the
// checker did not infer a type for this node; it does not mean the type is any.
func (types InferredTypes) TypeOf(node syntax.Node) (exprtypes.Type, bool) {
	ty, found := types.byNode[node]
	return ty, found
}
