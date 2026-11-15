package cachepolicy

import (
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// position uses the declaration's source position when present.
func position(node interface {
	Pos() (workflowlang.Pos, bool)
},
) workflowlang.Pos {
	pos, _ := node.Pos()
	return pos
}
