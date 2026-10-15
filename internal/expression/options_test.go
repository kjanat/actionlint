package expression

import "testing"

func TestCheckSelectionWithoutRepositoryConfig(t *testing.T) {
	for _, tc := range []struct {
		source, rule string
		checks       Checks
	}{
		{"env.FLAG", "string-conditions", Checks{StringConditions: true}},
		{"github.ref == 1", "mixed-type-comparisons", Checks{MixedTypeComparisons: true}},
		{"github.ref == 'main'", "case-insensitive-conditions", Checks{CaseInsensitiveConditions: true}},
	} {
		for _, enabled := range []bool{false, true} {
			checks := Checks{}
			if enabled {
				checks = tc.checks
			}
			checker := NewChecker(false, &Options{Checks: checks})
			checker.SetWorkflowKeyAvailability("jobs.<job_id>.steps.if")
			node, err, _ := ParseConditionExpression(tc.source)
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
