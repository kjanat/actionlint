package conventions_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/rules/conventions"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestJobTimeoutCheckOwnsConfiguredBounds(t *testing.T) {
	t.Parallel()
	const source = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    timeout-minutes: 10\n    steps: [{run: echo ok}]\n"
	parsed, errs := workflowparser.Parse([]byte(source))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	job, found := declaration.Publish(source, parsed, false, "").Bind("test").Workflow().Job("build")
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
			check := conventions.JobTimeoutCheck(policy)
			limit = 10
			findings := check.Job(job)
			if len(findings) != 1 || findings[0].Rule != "job-timeout" {
				t.Fatalf("caller mutation changed the configured %s: %+v", bound, findings)
			}
		})
	}
}
