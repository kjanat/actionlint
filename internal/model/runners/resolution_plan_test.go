package runner

import (
	"testing"

	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
)

func TestResolutionPlanOwnsProviderInputsAndAnswers(t *testing.T) {
	t.Parallel()
	workflow := runnerWorkflow(t, "{group: build, labels: queue}")
	descriptor := runnerapi.Descriptor{Name: "queues", Targets: []string{"custom"}}
	plan, err := NewResolutionPlan(workflow.Snapshot, KnownLabels(nil), runnerapi.Request{Target: "custom", Scope: "scope"}, ProviderSelection{Descriptor: descriptor, ImplementationID: "implementation"}, false)
	if err != nil {
		t.Fatal(err)
	}
	request := plan.Request()
	request.Selectors[0].Labels[0].Value = runnerapi.Literal("changed")
	request.Selectors[0].Group.Value = runnerapi.Literal("changed")
	descriptor.Targets[0] = "changed"
	again := plan.Request()
	if again.Selectors[0].Labels[0].Value != runnerapi.Literal("queue") || again.Selectors[0].Group.Value != runnerapi.Literal("build") {
		t.Fatal("provider changed the captured selectors")
	}
	candidates := []runnerapi.Candidate{{Name: "queue", OS: runnerapi.Windows, DefaultShell: "pwsh", CaseSensitivity: runnerapi.CaseInsensitive}}
	answers := []runnerapi.Answer{{Resolution: &runnerapi.Resolved{Candidates: candidates}, DatasetStatus: runnerapi.DatasetStatus{DatasetID: "inventory", Scope: "scope", Freshness: runnerapi.Fresh, Complete: true}}}
	knowledge, err := plan.Complete(answers)
	if err != nil {
		t.Fatal(err)
	}
	candidates[0].DefaultShell = "changed"
	answers[0].DatasetStatus.DatasetID = "changed"
	job := knowledge.Job(workflow.Views["test"])
	report := knowledge.Report()
	if job.Shell != "pwsh" || report.Provider.Targets[0] != "custom" || report.Answers[0].DatasetStatus.DatasetID != "inventory" {
		t.Fatalf("provider changed stored knowledge: job=%+v report=%+v", job, report)
	}
}
