package actionlint

import (
	"strings"
	"unicode/utf8"
)

func metadataExpressionEndPosition(meta *ActionMetadata, value *ActionExprString, violation actionExpressionViolation) *Pos {
	if meta.positions == nil || violation.offset <= 0 || violation.endOffset <= violation.offset || violation.endOffset > len(value.Value)+1 {
		return nil
	}
	source := meta.positions.source(meta, value)
	coordinates := func(offset int) (int, int) {
		prefix := value.Value[:offset-1]
		return strings.Count(prefix, "\n") + 1, utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:]) + 1
	}
	line, column := coordinates(violation.offset)
	if _, ok := source.pos(line, column); !ok {
		return nil
	}
	line, column = coordinates(violation.endOffset)
	if end, ok := source.endPos(line, column); ok {
		return end
	}
	return nil
}
