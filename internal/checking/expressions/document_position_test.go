package expressions

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
)

func TestDocumentPositionOffsetsOnlyTheFirstLineColumn(t *testing.T) {
	t.Parallel()
	base := sourcetext.Position{Line: 5, Column: 10}
	testCases := []struct {
		what string
		at   sourcetext.Position
		want sourcetext.Position
	}{
		{what: "first line", at: sourcetext.Position{Line: 1, Column: 3}, want: sourcetext.Position{Line: 5, Column: 12}},
		{what: "second line", at: sourcetext.Position{Line: 2, Column: 3}, want: sourcetext.Position{Line: 6, Column: 3}},
		{what: "third line start", at: sourcetext.Position{Line: 3, Column: 1}, want: sourcetext.Position{Line: 7, Column: 1}},
	}
	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			if got := documentPosition(base, tc.at); got != tc.want {
				t.Fatalf("documentPosition(%v, %v) = %v, want %v", base, tc.at, got, tc.want)
			}
		})
	}
}
