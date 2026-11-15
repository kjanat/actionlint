package policy_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestEqualityRetainsDiagnosticIdentity(t *testing.T) {
	t.Parallel()
	base := finding.Finding{Rule: "first", Path: "ci.yml", Message: "same", Start: sourcepos.Position{Line: 1, Column: 1}}
	for _, change := range []struct {
		name string
		edit func(*finding.Finding)
	}{
		{"rule", func(e *finding.Finding) { e.Rule = "second" }},
		{"code", func(e *finding.Finding) { e.Code = "E001" }},
		{"inclusive end", func(e *finding.Finding) { e.End.Column = 6 }},
		{"end", func(e *finding.Finding) { e.End = sourcepos.Position{Line: 2, Column: 1} }},
		{"fix", func(e *finding.Finding) { e.Fixes = []results.Fix{{Description: "fix"}} }},
	} {
		t.Run(change.name, func(t *testing.T) {
			t.Parallel()
			other := base
			change.edit(&other)
			input := []*finding.Finding{base.Clone(), &other}
			rules := checking.ConfigurationRules()
			p := preparePolicy(rules, nil, policy.Policy{Path: "ci.yml", Content: []byte("long enough token\nnext line\n")}, nil, input)
			result, err := policy.Publish([]policy.Batch{{Processor: policy.Processor{RuleSet: rules}, Policy: p, Findings: input}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 2 {
				t.Fatalf("distinct %s incorrectly deduplicated", change.name)
			}
		})
	}
}
