package facts

import (
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
)

func Position(pos *sourcepos.Position) sourcepos.Position {
	if pos == nil {
		return sourcepos.Position{}
	}
	return sourcepos.Position{Line: pos.Line, Column: pos.Column}
}
