package facts

import (
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func Position(pos *workflowlang.Pos) sourcepos.Position {
	if pos == nil {
		return sourcepos.Position{}
	}
	return sourcepos.Position{Line: pos.Line, Column: pos.Col}
}
