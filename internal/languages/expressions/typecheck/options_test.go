package typecheck

import (
	"testing"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func TestCheckSelectionWithoutRepositoryConfig(t *testing.T) {
	for _, tc := range []struct {
		source, rule string
		checks       Checks
	}{
		{"github.ref == 'main'", "case-insensitive-conditions", Checks{CaseInsensitiveConditions: true}},
	} {
		for _, enabled := range []bool{false, true} {
			checks := Checks{}
			if enabled {
				checks = tc.checks
			}
			checker := NewChecker(&Options{Checks: checks})
			checker.SetWorkflowKeyAvailability("jobs.<job_id>.steps.if")
			node, err, _ := exprsyntax.ParseConditionExpression(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			_, findings := checker.Check(node)
			count := 0
			for _, finding := range findings {
				if finding.Rule == tc.rule {
					count++
				} else {
					t.Errorf("unexpected finding: %s", finding)
				}
			}
			want := 0
			if enabled {
				want = 1
			}
			if count != want {
				t.Errorf("%s enabled=%t: got %d findings, want %d", tc.rule, enabled, count, want)
			}
		}
	}
}
