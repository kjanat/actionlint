package cachepolicy

import "actionlint.kjanat.dev/v2/internal/input/source"

// position uses the declaration's source position when present.
func position(pos *source.Position) source.Position {
	if pos == nil {
		return source.Position{}
	}
	return *pos
}
