package facts

import (
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
)

func Position(pos *workflowlang.Pos) results.Position {
	if pos == nil {
		return results.Position{}
	}
	return results.Position{Line: pos.Line, Column: pos.Col}
}
