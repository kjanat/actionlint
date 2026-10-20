package shellcheck

import (
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/yamlsource"
)

func applyDiagnosticFix(t *testing.T, input string, fix DiagnosticFix) string {
	t.Helper()
	offset := func(pos DiagnosticPosition) int {
		start := 0
		for line := 1; line < pos.Line; line++ {
			i := strings.IndexByte(input[start:], '\n')
			if i < 0 {
				t.Fatalf("edit line outside source: %+v", pos)
			}
			start += i + 1
		}
		line, _, _ := strings.Cut(input[start:], "\n")
		i, ok := yamlsource.ByteOffsetAtColumn(line, pos.Column)
		if !ok {
			t.Fatalf("edit column outside source: %+v", pos)
		}
		return start + i
	}
	result := input
	for _, edit := range slices.Backward(fix.Edits) {
		start, end := offset(edit.Start), offset(edit.End)
		result = result[:start] + edit.Replacement + result[end:]
	}
	return result
}
