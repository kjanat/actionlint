package finding

import (
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestEqualityRetainsDiagnosticIdentity(t *testing.T) {
	base := Finding{Rule: "first", Path: "ci.yml", Message: "same", Start: diagnostic.Position{Line: 1, Column: 1}}
	for _, change := range []struct {
		name string
		edit func(*Finding)
	}{
		{"rule", func(e *Finding) { e.Rule = "second" }},
		{"code", func(e *Finding) { e.Code = "E001" }},
		{"inclusive end", func(e *Finding) { e.EndColumn = 5 }},
		{"end", func(e *Finding) { e.EndPosition = &workflowlang.Pos{Line: 2, Col: 1} }},
		{"fix", func(e *Finding) { e.Fixes = []diagnostic.Fix{{Description: "fix"}} }},
	} {
		t.Run(change.name, func(t *testing.T) {
			other := base
			change.edit(&other)
			if Equal(&base, &other) {
				t.Fatalf("distinct %s incorrectly deduplicated", change.name)
			}
		})
	}
}
