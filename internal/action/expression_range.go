package action

import (
	"strings"
	"unicode/utf8"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

// ExpressionEndPosition returns a verified exclusive end, or nil without a mapping.
func ExpressionEndPosition(meta *Metadata, value *ExprString, violation Violation) *workflowlang.Pos {
	if violation.Offset <= 0 || violation.EndOffset < violation.Offset || violation.EndOffset > len(value.Value)+1 {
		return nil
	}
	source := meta.Provenance.source(value)
	coordinates := func(offset int) (int, int) {
		prefix := value.Value[:offset-1]
		return strings.Count(prefix, "\n") + 1, utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:]) + 1
	}
	line, column := coordinates(violation.Offset)
	if violation.Offset == len(value.Value)+1 && violation.EndOffset == violation.Offset {
		if end, ok := source.EndPos(line, column); ok {
			return end
		}
		return nil
	}
	if _, ok := source.Position(line, column); !ok {
		return nil
	}
	line, column = coordinates(violation.EndOffset)
	if end, ok := source.EndPos(line, column); ok {
		return end
	}
	return nil
}
