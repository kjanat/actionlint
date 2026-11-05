package rules_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/parsing"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

func matrixJob(t testing.TB, snapshot parsing.SourceSnapshot, key string) workflowdecl.JobView {
	t.Helper()
	workflow, present := snapshot.WorkflowView()
	if !present {
		t.Fatal("captured workflow absent")
	}
	job, present := workflow.Job(key)
	if !present {
		t.Fatalf("captured job %q absent", key)
	}
	return job
}
