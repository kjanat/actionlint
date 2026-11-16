package runners

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/services"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

func lifecycleRunnerKnowledge(t *testing.T, date string, candidates []runnerapi.Candidate, policies []runnerapi.SupportPolicy) (*Knowledge, workflows.Job) {
	t.Helper()
	workflow := runnerWorkflow(t, "queue")
	service, err := services.ParseService("github.com", "")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewResolutionPlan(workflow.Snapshot, KnownLabels(nil), runnerapi.Request{Target: "custom", Scope: "scope", Service: service, AsOf: supportDate(date)}, ProviderSelection{}, false)
	if err != nil {
		t.Fatal(err)
	}
	response := runnerapi.Response{
		Answers: []runnerapi.Answer{{Resolution: runnerapi.Resolved{Candidates: candidates}, DatasetStatus: runnerapi.DatasetStatus{DatasetID: "inventory", Scope: "scope", Freshness: runnerapi.Fresh, Complete: true}}},
		Support: runnerapi.SupportCatalog{DatasetStatus: runnerapi.DatasetStatus{DatasetID: "support", Scope: "scope", Freshness: runnerapi.Fresh, Complete: true}, Policies: policies},
	}
	knowledge, err := plan.Complete(response)
	if err != nil {
		t.Fatal(err)
	}
	return knowledge, workflow.Views["test"]
}

func TestSupportLifecycleCurrentAndFutureEvents(t *testing.T) {
	t.Parallel()
	change := func(date string, state runnerapi.SupportState) runnerapi.SupportChange {
		return runnerapi.SupportChange{On: supportDate(date), State: state}
	}
	for _, tc := range []struct {
		name              string
		initial           runnerapi.SupportState
		changes           []runnerapi.SupportChange
		current, upcoming int
		kind              lifecycle.EventKind
		effective         string
	}{
		{"announced deadline", runnerapi.Supported, []runnerapi.SupportChange{change("2026-12-01", runnerapi.Deprecated), change("2026-12-08", runnerapi.Unsupported)}, 0, 1, lifecycle.SupportEnds, "2026-12-08"},
		{"unknown initial deadline", runnerapi.SupportUnknown, []runnerapi.SupportChange{change("2026-12-08", runnerapi.Unsupported)}, 0, 1, lifecycle.SupportEnds, "2026-12-08"},
		{"future introduction", runnerapi.SupportUnknown, []runnerapi.SupportChange{change("2026-12-01", runnerapi.Supported), change("2026-12-08", runnerapi.Unsupported)}, 0, 1, lifecycle.SupportStarts, "2026-12-01"},
		{"restoration stops scan", runnerapi.Supported, []runnerapi.SupportChange{change("2026-12-01", runnerapi.Deprecated), change("2026-12-02", runnerapi.Supported), change("2026-12-08", runnerapi.Unsupported)}, 0, 0, "", ""},
		{"unknown stops scan", runnerapi.Supported, []runnerapi.SupportChange{change("2026-12-01", runnerapi.SupportUnknown), change("2026-12-08", runnerapi.Unsupported)}, 0, 0, "", ""},
		{"deprecated now", runnerapi.Deprecated, []runnerapi.SupportChange{change("2026-12-08", runnerapi.Unsupported)}, 1, 0, lifecycle.SupportEnds, "2026-12-08"},
		{"repeated deprecation before deadline", runnerapi.Deprecated, []runnerapi.SupportChange{change("2026-12-01", runnerapi.Deprecated), change("2026-12-08", runnerapi.Unsupported)}, 1, 0, lifecycle.SupportEnds, "2026-12-08"},
		{"known retirement before restoration", runnerapi.Supported, []runnerapi.SupportChange{change("2026-11-01", runnerapi.Unsupported), change("2026-12-01", runnerapi.Supported)}, 1, 0, lifecycle.SupportEnds, "2026-11-01"},
		{"initially unavailable", runnerapi.Unsupported, []runnerapi.SupportChange{change("2026-12-01", runnerapi.Supported)}, 1, 0, lifecycle.SupportStarts, "2026-12-01"},
		{"undated unsupported", runnerapi.Unsupported, nil, 1, 0, lifecycle.SupportEnds, ""},
		{"undated deprecation", runnerapi.Deprecated, nil, 1, 0, lifecycle.SupportEnds, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			policy := runnerapi.SupportPolicy{ID: "test-policy", Description: "test runner", URL: "https://example.com/support", Capability: runnerapi.JobExecution, Initial: tc.initial, Changes: tc.changes}
			k, job := lifecycleRunnerKnowledge(t, "2026-11-15", []runnerapi.Candidate{{Name: "one", Hosting: runnerapi.Hosted, Architecture: "arm32"}}, []runnerapi.SupportPolicy{policy})
			current, upcoming := k.Support(job, runnerapi.JobExecution), k.UpcomingSupport(job, runnerapi.JobExecution)
			if len(current) != tc.current || len(upcoming) != tc.upcoming {
				t.Fatalf("current=%+v upcoming=%+v", current, upcoming)
			}
			for _, p := range append(current, upcoming...) {
				e := p.Evidence
				if e == nil || e.Subject != lifecycle.Runners || e.Kind != tc.kind || e.Effective.String() != tc.effective || e.Current != (tc.current > 0) || !e.Announced.IsZero() {
					t.Fatalf("incorrect evidence: %+v", e)
				}
			}
		})
	}
}

func TestUpcomingSupportRequiresOnePolicyToCoverEveryCandidate(t *testing.T) {
	t.Parallel()
	future := func(id, arch string) runnerapi.SupportPolicy {
		return runnerapi.SupportPolicy{ID: id, Description: id, URL: "https://example.com/" + id, Capability: runnerapi.JobExecution, Architectures: []string{arch}, Initial: runnerapi.Supported, Changes: []runnerapi.SupportChange{{On: supportDate("2026-12-08"), State: runnerapi.Unsupported}}}
	}
	arm, x64 := future("arm-policy", "arm32"), future("x64-policy", "x64")
	currentArm := arm
	currentArm.Initial = runnerapi.Unsupported
	currentArm.Changes = nil
	currentX64 := x64
	currentX64.Initial = runnerapi.Unsupported
	currentX64.Changes = nil
	for _, tc := range []struct {
		name              string
		candidates        []runnerapi.Candidate
		policies          []runnerapi.SupportPolicy
		current, upcoming int
	}{
		{"one policy covers all", []runnerapi.Candidate{{Name: "a", Architecture: "arm32"}, {Name: "b", Architecture: "arm32"}}, []runnerapi.SupportPolicy{arm}, 0, 1},
		{"future split", []runnerapi.Candidate{{Name: "a", Architecture: "arm32"}, {Name: "b", Architecture: "x64"}}, []runnerapi.SupportPolicy{arm, x64}, 0, 0},
		{"current and future split", []runnerapi.Candidate{{Name: "a", Architecture: "arm32"}, {Name: "b", Architecture: "x64"}}, []runnerapi.SupportPolicy{currentArm, x64}, 0, 0},
		{"legacy current joint coverage", []runnerapi.Candidate{{Name: "a", Architecture: "arm32"}, {Name: "b", Architecture: "x64"}}, []runnerapi.SupportPolicy{currentArm, currentX64}, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			k, job := lifecycleRunnerKnowledge(t, "2026-11-15", tc.candidates, tc.policies)
			current, upcoming := k.Support(job, runnerapi.JobExecution), k.UpcomingSupport(job, runnerapi.JobExecution)
			if len(current) != tc.current || len(upcoming) != tc.upcoming {
				t.Fatalf("current=%+v upcoming=%+v", current, upcoming)
			}
			if tc.name == "legacy current joint coverage" {
				for _, p := range current {
					if p.Evidence == nil || len(p.Evidence.AllOf) != 2 {
						t.Fatal("joint policy coverage lost its universal candidate proof")
					}
				}
			}
		})
	}
}

func TestCurrentJointSupportNotificationRequiresEveryCandidate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		initial       runnerapi.SupportState
		first, second string
		start         lifecycle.Start
		active        bool
	}{
		{"retired candidates", runnerapi.Supported, "2026-11-01", "2026-11-10", lifecycle.OnEffective(), true},
		{"one outside window", runnerapi.Deprecated, "2026-12-08", "2027-12-08", lifecycle.Before(90), false},
		{"both inside window", runnerapi.Deprecated, "2026-12-08", "2027-01-08", lifecycle.Before(90), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			policies := []runnerapi.SupportPolicy{
				{ID: "arm-policy", Description: "ARM", URL: "https://example.com/arm", Capability: runnerapi.JobExecution, Architectures: []string{"arm32"}, Initial: tc.initial, Changes: []runnerapi.SupportChange{{On: supportDate(tc.first), State: runnerapi.Unsupported}}},
				{ID: "x64-policy", Description: "x64", URL: "https://example.com/x64", Capability: runnerapi.JobExecution, Architectures: []string{"x64"}, Initial: tc.initial, Changes: []runnerapi.SupportChange{{On: supportDate(tc.second), State: runnerapi.Unsupported}}},
			}
			k, job := lifecycleRunnerKnowledge(t, "2026-11-15", []runnerapi.Candidate{{Name: "arm", Architecture: "arm32"}, {Name: "x64", Architecture: "x64"}}, policies)
			problems := k.Support(job, runnerapi.JobExecution)
			if len(problems) != 2 {
				t.Fatalf("joint support missing: %+v", problems)
			}
			policy, err := lifecycle.NewPolicy("required-migration", "Migrate supported runners", lifecycle.Match{Subjects: []lifecycle.Subject{lifecycle.Runners}, Events: []lifecycle.EventKind{lifecycle.SupportEnds}}, []lifecycle.Notification{{Starts: tc.start, Severity: lifecycle.Error}}, "organization.toml")
			if err != nil {
				t.Fatal(err)
			}
			for _, problem := range problems {
				if problem.Evidence == nil || len(problem.Evidence.AllOf) != 2 {
					t.Fatalf("no candidate proof: %+v", problem)
				}
				assessment := lifecycle.Assess(*problem.Evidence, []lifecycle.Policy{policy})
				if (len(assessment.Active) > 0) != tc.active {
					t.Fatalf("%s: %+v", tc.name, assessment)
				}
				if tc.active && assessment.Severity != lifecycle.Error {
					t.Fatalf("required minimum lost: %+v", assessment)
				}
			}
		})
	}
}

func TestBuiltinSupportAnnouncementEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, date, announced, effective string
		hosting                          runnerapi.Hosting
		current                          bool
	}{
		{"self-hosted before amendment", "2026-08-24", "2026-08-25", "2026-09-23", runnerapi.SelfHosted, false},
		{"hosted before announcement", "2026-11-11", "2026-11-12", "2026-12-08", runnerapi.Hosted, false},
		{"hosted deprecation", "2026-11-12", "2026-11-12", "2026-12-08", runnerapi.Hosted, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			k, job := lifecycleRunnerKnowledge(t, tc.date, []runnerapi.Candidate{{Name: "arm", Hosting: tc.hosting, OS: runnerapi.Linux, Architecture: "arm32"}}, builtinSupport("scope").Policies)
			all := append(k.Support(job, runnerapi.JobExecution), k.UpcomingSupport(job, runnerapi.JobExecution)...)
			if len(all) != 1 || all[0].Evidence == nil {
				t.Fatalf("missing evidence: %+v", all)
			}
			e := all[0].Evidence
			if e.Announced.String() != tc.announced || e.Effective.String() != tc.effective || e.Current != tc.current {
				t.Fatalf("incorrect built-in dates: %+v", e)
			}
		})
	}
}
