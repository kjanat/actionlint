package rules_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/parsing"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

func matrixJob(tb testing.TB, snapshot parsing.SourceSnapshot, key string) workflowdecl.JobView {
	tb.Helper()
	workflow, present := snapshot.WorkflowView()
	if !present {
		tb.Fatal("captured workflow absent")
	}
	job, present := workflow.Job(key)
	if !present {
		tb.Fatalf("captured job %q absent", key)
	}
	return job
}
