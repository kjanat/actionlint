package runners

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/services"
)

func TestSupportPolicyVersionBoundaries(t *testing.T) {
	t.Parallel()
	scope := runnerapi.ServiceScope{Kind: services.GitHubEnterpriseServer, MinVersion: "3.9", BeforeVersion: "3.20"}
	for _, tc := range []struct {
		version string
		want    bool
	}{{"", false}, {"3.8", false}, {"3.9", true}, {"3.19.10", true}, {"3.20", false}, {"3.019", true}} {
		service, err := services.ParseService("ghes", tc.version)
		if err != nil {
			t.Fatal(err)
		}
		if got := serviceMatches(scope, service); got != tc.want {
			t.Errorf("version %q: got %t want %t", tc.version, got, tc.want)
		}
	}
}

func TestSupportCatalogDetachedAndCacheIdentity(t *testing.T) {
	t.Parallel()
	workflow := runnerWorkflow(t, "queue")
	service, err := services.ParseService("github.com", "")
	if err != nil {
		t.Fatal(err)
	}
	request := runnerapi.Request{Target: "custom", Scope: "scope", Service: service, AsOf: supportDate("2026-11-14")}
	plan, err := NewResolutionPlan(workflow.Snapshot, KnownLabels(nil), request, ProviderSelection{Descriptor: runnerapi.Descriptor{Name: "custom", Targets: []string{"custom"}}, ImplementationID: "impl"}, false)
	if err != nil {
		t.Fatal(err)
	}
	response := runnerapi.Response{Answers: []runnerapi.Answer{{Resolution: runnerapi.Resolved{Candidates: []runnerapi.Candidate{{Name: "q", Hosting: runnerapi.SelfHosted, Architecture: "arm"}}}, DatasetStatus: runnerapi.DatasetStatus{DatasetID: "inventory", Scope: "scope", Freshness: runnerapi.Fresh, Complete: true}}}, Support: builtinSupport("scope")}
	knowledge, err := plan.Complete(response)
	if err != nil {
		t.Fatal(err)
	}
	before := knowledge.Report()
	response.Support.Policies[0].Description = "modified"
	response.Support.Policies[0].Changes[0].State = runnerapi.Supported
	problems := knowledge.Support(workflow.Views["test"], runnerapi.JobExecution)
	if len(problems) != 1 || problems[0].Policy.Description == "modified" || problems[0].State != runnerapi.Unsupported {
		t.Fatalf("mutation changed knowledge: %+v", problems)
	}
	problems[0].Policy.Changes[0].State = runnerapi.Supported
	report := knowledge.Report()
	report.Support.Policies[0].Changes[0].State = runnerapi.Supported
	if got := knowledge.Support(workflow.Views["test"], runnerapi.JobExecution); len(got) != 1 || got[0].State != runnerapi.Unsupported {
		t.Fatalf("reader changed knowledge: %+v", got)
	}
	other, err := plan.Complete(response)
	if err != nil {
		t.Fatal(err)
	}
	if before.CacheKey == other.Report().CacheKey {
		t.Fatal("support change did not change cache key")
	}
	request.AsOf = supportDate("2026-11-15")
	later, err := NewResolutionPlan(workflow.Snapshot, KnownLabels(nil), request, ProviderSelection{Descriptor: runnerapi.Descriptor{Name: "custom", Targets: []string{"custom"}}, ImplementationID: "impl"}, false)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := later.Complete(response)
	if err != nil {
		t.Fatal(err)
	}
	if other.Report().CacheKey == changed.Report().CacheKey {
		t.Fatal("date change did not change cache key")
	}
}

func TestSupportRequiresEveryCandidateToBeAffected(t *testing.T) {
	t.Parallel()
	w := runnerWorkflow(t, "queue")
	svc, err := services.ParseService("github.com", "")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewResolutionPlan(w.Snapshot, KnownLabels(nil), runnerapi.Request{Target: "custom", Scope: "scope", Service: svc, AsOf: supportDate("2026-12-08")}, ProviderSelection{}, false)
	if err != nil {
		t.Fatal(err)
	}
	response := runnerapi.Response{Answers: []runnerapi.Answer{{Resolution: runnerapi.Resolved{Candidates: []runnerapi.Candidate{{Name: "a", Hosting: runnerapi.SelfHosted, Architecture: "arm32"}, {Name: "b", Hosting: runnerapi.SelfHosted, Architecture: "x64"}}}, DatasetStatus: runnerapi.DatasetStatus{DatasetID: "inventory", Scope: "scope", Freshness: runnerapi.Fresh, Complete: true}}}, Support: runnerapi.SupportCatalog{DatasetStatus: runnerapi.DatasetStatus{DatasetID: "policy", Scope: "scope", Freshness: runnerapi.Fresh, Complete: true}, Policies: []runnerapi.SupportPolicy{{ID: "first", Description: "arm", URL: "https://example.com/arm", Capability: runnerapi.JobExecution, Architectures: []string{"arm"}, Initial: runnerapi.Unsupported}, {ID: "second", Description: "x64", URL: "https://example.com/x64", Capability: runnerapi.JobExecution, Architectures: []string{"x64"}, Initial: runnerapi.Deprecated}}}}
	k, err := plan.Complete(response)
	if err != nil {
		t.Fatal(err)
	}
	if got := k.Support(w.Views["test"], runnerapi.JobExecution); len(got) != 2 || got[0].Policy.Architectures[0] != "arm32" {
		t.Fatalf("distinct policies for all candidates: %+v", got)
	}
	response.Support.Policies = response.Support.Policies[:1]
	k, err = plan.Complete(response)
	if err != nil {
		t.Fatal(err)
	}
	if got := k.Support(w.Views["test"], runnerapi.JobExecution); len(got) != 0 {
		t.Fatalf("partially affected candidates: %+v", got)
	}
}
