package security

import (
	"reflect"
	"testing"

	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestCacheSecurityCallbacksDoNotRequireWorkflowVisits(t *testing.T) {
	t.Parallel()
	const source = `on: pull_request_target
cache-mode: write
jobs:
  z_first:
    uses: example/repo/.github/workflows/build.yml@v1
  a_second:
    uses: example/repo/.github/workflows/build.yml@v1
`
	decoded, errors := workflowparser.Parse([]byte(source))
	if len(errors) != 0 {
		t.Fatal(errors)
	}
	workflow := declaration.Publish(source, decoded, false, "").Bind("test").Workflow()
	first, _ := workflow.Job("z_first")
	second, _ := workflow.Job("a_second")
	check := CacheWriteUntrustedCheck(workflow)
	if check.Workflow != nil || check.Step != nil || check.Name != "cache-write-untrusted" {
		t.Fatalf("unexpected declaration callbacks: %+v", check)
	}
	if findings := check.Job(second); len(findings) != 0 {
		t.Fatalf("later job claimed the shared write grant: %+v", findings)
	}
	findings := check.Job(first)
	if len(findings) != 1 || findings[0].Rule != "cache-write-untrusted" || findings[0].Start.Line != 2 {
		t.Fatalf("shared declaration lost its source or rule: %+v", findings)
	}
	if repeated := check.Job(first); !reflect.DeepEqual(findings, repeated) {
		t.Fatal("repeated callback changed the finding")
	}
	// Without a declared ceiling, both reusable calls require their own finding.
	decoded.CacheMode = nil
	workflow = declaration.Publish(source, decoded, false, "").Bind("without-ceiling").Workflow()
	first, _ = workflow.Job("z_first")
	second, _ = workflow.Job("a_second")
	check = CacheCallUnrestrictedCheck(workflow)
	for _, job := range []declaration.JobView{second, first, second} {
		findings := check.Job(job)
		if len(findings) != 1 || findings[0].Rule != "cache-call-unrestricted" {
			t.Fatalf("call lost its independent ceiling check: %+v", findings)
		}
	}
}
