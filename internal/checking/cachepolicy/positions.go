package cachepolicy

import (
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// position uses the declaration's source position when present.
func position(pos *workflowlang.Pos) workflowlang.Pos {
	if pos == nil {
		return workflowlang.Pos{}
	}
	return *pos
}
