package conventions_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/rules/conventions"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestJobTimeoutChecksCopyBounds(t *testing.T) {
	t.Parallel()
	const source = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    timeout-minutes: 10\n    steps: [{run: echo ok}]\n"
	parsed, errs := parser.ParseWorkflow([]byte(source))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	job, found := workflows.Bind("test", source, parsed, false, "").Workflow().Job("build")
	if !found {
		t.Fatal("fixture job is missing")
	}
	for _, bound := range []string{"minimum", "maximum"} {
		t.Run(bound, func(t *testing.T) {
			t.Parallel()
			limit := 20.0
			policy := conventions.TimeoutPolicy{Enabled: true, Min: &limit}
			if bound == "maximum" {
				limit = 5
				policy.Min, policy.Max = nil, &limit
			}
			check := conventions.JobTimeoutChecks(policy)
			limit = 10
			findings := check.Job(job)
			if len(findings) != 1 {
				t.Fatalf("caller mutation changed the configured %s: %+v", bound, findings)
			}
		})
	}
}
