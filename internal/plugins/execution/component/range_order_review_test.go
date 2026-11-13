package component

import (
	"strings"
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"github.com/kjanat/wacogo"
)

func TestComponentRejectsReversedRanges(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		start, end sourcepos.Position
		valid      bool
	}{
		{"earlier line", sourcepos.Position{Line: 10, Column: 2}, sourcepos.Position{Line: 9, Column: 8}, false},
		{"earlier column", sourcepos.Position{Line: 10, Column: 8}, sourcepos.Position{Line: 10, Column: 2}, false},
		{"empty insertion", sourcepos.Position{Line: 10, Column: 2}, sourcepos.Position{Line: 10, Column: 2}, true},
		{"later column", sourcepos.Position{Line: 10, Column: 2}, sourcepos.Position{Line: 10, Column: 8}, true},
		{"later line", sourcepos.Position{Line: 9, Column: 8}, sourcepos.Position{Line: 10, Column: 2}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			t.Run("related", func(t *testing.T) {
				t.Parallel()
				_, err := liftRelated(wacogo.NewValListOf(wacogo.NewValRecord(
					field("message", wacogo.ValString("related")), field("path", wacogo.ValString("workflow.yml")),
					field("start", lowerPosition(tc.start)), field("end", lowerPosition(tc.end)),
				)))
				if tc.valid && err != nil || !tc.valid && (err == nil || !strings.Contains(err.Error(), "end precedes start")) {
					t.Fatalf("valid=%v: %v", tc.valid, err)
				}
			})
			for _, edit := range []bool{false, true} {
				t.Run(map[bool]string{false: "diagnostic", true: "edit"}[edit], func(t *testing.T) {
					t.Parallel()
					var err error
					if edit {
						_, err = liftEdits(wacogo.NewValListOf(wacogo.NewValRecord(
							field("path", wacogo.ValString("workflow.yml")),
							field("start", lowerPosition(tc.start)), field("end", lowerPosition(tc.end)),
							field("replacement", wacogo.ValString("replacement")),
						)))
					} else {
						_, err = liftDiagnostic(wacogo.NewValRecord(
							field("reference", wacogo.ValOptionNone()), field("related", wacogo.NewValListOf[wacogo.Val]()),
							field("explanation", wacogo.ValString("")), field("help", wacogo.ValString("")), field("documentation-url", wacogo.ValString("")),
							field("rule", wacogo.ValString("example")), field("code", wacogo.ValString("EX001")),
							field("severity", wacogo.ValString("warning")), field("message", wacogo.ValString("finding")),
							field("path", wacogo.ValString("workflow.yml")),
							field("start", lowerPosition(tc.start)), field("end", lowerPosition(tc.end)),
							field("fixes", wacogo.NewValListOf[wacogo.Val]()),
						))
					}
					if tc.valid && err != nil || !tc.valid && (err == nil || !strings.Contains(err.Error(), "end precedes start")) {
						t.Fatalf("edit=%v valid=%v: %v", edit, tc.valid, err)
					}
				})
			}
		})
	}
}
