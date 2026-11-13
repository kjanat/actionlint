package finding

import (
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestEqualityRetainsDiagnosticIdentity(t *testing.T) {
	t.Parallel()
	base := Finding{Rule: "first", Path: "ci.yml", Message: "same", Start: sourcepos.Position{Line: 1, Column: 1}}
	for _, change := range []struct {
		name string
		edit func(*Finding)
	}{
		{"rule", func(e *Finding) { e.Rule = "second" }},
		{"code", func(e *Finding) { e.Code = "E001" }},
		{"inclusive end", func(e *Finding) { e.EndColumn = 5 }},
		{"end", func(e *Finding) { e.EndPosition = &workflowlang.Pos{Line: 2, Col: 1} }},
		{"fix", func(e *Finding) { e.Fixes = []results.Fix{{Description: "fix"}} }},
	} {
		t.Run(change.name, func(t *testing.T) {
			t.Parallel()
			other := base
			change.edit(&other)
			if Equal(&base, &other) {
				t.Fatalf("distinct %s incorrectly deduplicated", change.name)
			}
		})
	}
}
