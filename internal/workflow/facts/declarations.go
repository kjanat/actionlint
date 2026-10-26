package facts

import (
	"actionlint.kjanat.dev/v2/diagnostic"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func Position(pos *workflowlang.Pos) diagnostic.Position {
	if pos == nil {
		return diagnostic.Position{}
	}
	return diagnostic.Position{Line: pos.Line, Column: pos.Col}
}
