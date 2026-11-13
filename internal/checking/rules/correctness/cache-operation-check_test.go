package correctness

import (
	"testing"

	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestCacheOperationStepCallbackUsesItsDeclaringJob(t *testing.T) {
	t.Parallel()
	const source = `on: push
cache-mode: read
jobs:
  writable:
    runs-on: ubuntu-latest
    cache-mode: write
    steps: [{uses: actions/cache/save@v4}]
  inherited:
    runs-on: ubuntu-latest
    steps: [{uses: actions/cache/save@v4}]
`
	decoded, errors := workflowparser.Parse([]byte(source))
	if len(errors) != 0 {
		t.Fatal(errors)
	}
	workflow := declaration.Publish(source, decoded, false, "").Bind("test").Workflow()
	writable, _ := workflow.Job("writable")
	inherited, _ := workflow.Job("inherited")
	check := CacheOperationCheck(workflow, nil)
	for range 2 {
		for step := range inherited.Steps() {
			findings := check.Step(step)
			if len(findings) != 1 || findings[0].Rule != "cache-operation" || findings[0].Step != step || findings[0].Start.Line != 10 {
				t.Fatalf("inherited restriction lost its finding or step owner: %+v", findings)
			}
		}
		for step := range writable.Steps() {
			if findings := check.Step(step); len(findings) != 0 {
				t.Fatalf("another job's restriction leaked into this step: %+v", findings)
			}
		}
		if findings := check.Job(writable); len(findings) != 0 {
			t.Fatalf("absent metadata lookup produced findings: %+v", findings)
		}
	}
}
