package sourcemap

import (
	"testing"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/source/text"
)

func TestIndexedColumnsMatchRawCodepointColumns(t *testing.T) {
	for _, line := range []string{"", "abc", "é🐚x", "a\xffb", "a\u0301z"} {
		document := NewDocument(line)
		for column := 0; column <= utf8.RuneCountInString(line)+2; column++ {
			indexed, indexedOK := document.ByteOffset(0, column)
			raw, rawOK := text.ByteOffsetAtColumn(line, column)
			if indexedOK != rawOK || indexedOK && indexed != raw {
				t.Errorf("line %q column %d: indexed=(%d,%v), raw=(%d,%v)", line, column, indexed, indexedOK, raw, rawOK)
			}
		}
	}
}
