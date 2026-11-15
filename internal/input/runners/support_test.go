package runners

import (
	"context"
	"fmt"
	"strings"
	"testing"

	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/services"
)

type supportProvider struct{ response runnerapi.Response }

func (*supportProvider) RunnerDescriptor() runnerapi.Descriptor {
	return runnerapi.Descriptor{Name: "custom", Version: "1", Targets: []string{"github"}}
}

func (p *supportProvider) ResolveRunners(_ context.Context, r runnerapi.Request) (runnerapi.Response, error) {
	response := p.response
	response.Answers = make([]runnerapi.Answer, len(r.Selectors))
	for i := range response.Answers {
		response.Answers[i] = runnerapi.Answer{Resolution: p.response.Answers[0].Resolution, DatasetStatus: runnerapi.DatasetStatus{DatasetID: "custom", Scope: r.Scope, Freshness: runnerapi.Fresh, Complete: true}}
	}
	if response.Support.DatasetStatus.DatasetID != "" {
		response.Support.DatasetStatus.Scope = r.Scope
	}
	return response, nil
}

func date(t *testing.T, s string) runnerapi.Date {
	t.Helper()
	d, err := runnerapi.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func service(t *testing.T, kind, version string) *services.Service {
	t.Helper()
	s, err := services.Parse(kind, version)
	if err != nil {
		t.Fatal(err)
	}
	return &s
}

func TestRunnerSupportServiceAndArchitecture(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, selector, kind, version, day string
		want                               int
	}{
		{"unknown service", "[self-hosted, linux, ARM]", "", "", "2026-10-01", 0},
		{"cloud before", "[self-hosted, linux, ARM]", "github.com", "", "2026-09-22", 0},
		{"cloud cutoff", "[self-hosted, linux, ARM]", "github.com", "", "2026-09-23", 1},
		{"data residency", "[self-hosted, linux, ARM]", "github-data-residency", "", "2026-11-14", 1},
		{"ghes", "[self-hosted, linux, ARM]", "ghes", "3.19", "2026-11-14", 0},
		{"arm64", "[self-hosted, linux, ARM64]", "github.com", "", "2026-11-14", 0},
		{"hosted linux arm64", "ubuntu-24.04-arm", "github.com", "", "2026-12-09", 0},
		{"hosted windows arm64", "windows-11-arm", "github.com", "", "2026-12-09", 0},
		{"dynamic", "${{ vars.runner }}", "github.com", "", "2026-11-14", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := runnerWorkflow(t, tc.selector)
			k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{Service: service(t, tc.kind, tc.version), AsOf: date(t, tc.day)}, "scope")
			if err != nil {
				t.Fatal(err)
			}
			got := k.Support(w.Views["test"], runnerapi.JobExecution)
			if len(got) != tc.want {
				t.Fatalf("support=%+v want %d", got, tc.want)
			}
		})
	}
}

func TestRunnerSupportCandidateEvidenceAndDates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		candidates []runnerapi.Candidate
		kind, day  string
		capability runnerapi.Capability
		state      runnerapi.SupportState
	}{
		{"hosted before announcement", []runnerapi.Candidate{{Hosting: runnerapi.Hosted, OS: runnerapi.Linux, Architecture: "arm32"}}, "github.com", "2026-11-11", runnerapi.JobExecution, runnerapi.SupportUnknown},
		{"hosted deadline announced", []runnerapi.Candidate{{Hosting: runnerapi.Hosted, OS: runnerapi.Linux, Architecture: "armv7l"}}, "github.com", "2026-11-12", runnerapi.JobExecution, runnerapi.Deprecated},
		{"hosted deadline", []runnerapi.Candidate{{Hosting: runnerapi.Hosted, OS: runnerapi.Windows, Architecture: "arm32"}}, "github.com", "2026-12-08", runnerapi.JobExecution, runnerapi.Unsupported},
		{"hosted data residency unknown", []runnerapi.Candidate{{Hosting: runnerapi.Hosted, OS: runnerapi.Windows, Architecture: "arm32"}}, "github-data-residency", "2026-12-08", runnerapi.JobExecution, runnerapi.SupportUnknown},
		{"mixed architectures", []runnerapi.Candidate{{Hosting: runnerapi.SelfHosted, Architecture: "arm"}, {Hosting: runnerapi.SelfHosted, Architecture: "arm64"}}, "github.com", "2026-12-08", runnerapi.JobExecution, runnerapi.SupportUnknown},
		{"unknown candidate", []runnerapi.Candidate{{Hosting: runnerapi.SelfHosted, Architecture: "arm32"}, {Hosting: runnerapi.SelfHosted}}, "github.com", "2026-12-08", runnerapi.JobExecution, runnerapi.SupportUnknown},
		{"unknown hosting", []runnerapi.Candidate{{Architecture: "arm32"}}, "github.com", "2026-12-08", runnerapi.JobExecution, runnerapi.SupportUnknown},
		{"userspace capability", []runnerapi.Candidate{{Hosting: runnerapi.Hosted, OS: runnerapi.Linux, Architecture: "arm64"}}, "github.com", "2026-12-08", runnerapi.NativeAArch32Userspace, runnerapi.Unsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := runnerWorkflow(t, "queue")
			snapshot := fmt.Sprintf(`{"runners":[%s]}`, inventoryEntries(tc.candidates))
			opts := runnerapi.Options{Provider: "snapshot", Service: service(t, tc.kind, ""), AsOf: date(t, tc.day), Snapshot: runnerapi.Snapshot{Content: snapshot, Scope: "scope", Freshness: runnerapi.Fresh, Complete: true}}
			k, err := Prepare(t.Context(), w.Snapshot, nil, opts, "scope")
			if err != nil {
				t.Fatal(err)
			}
			got := k.Support(w.Views["test"], tc.capability)
			if tc.state == runnerapi.SupportUnknown {
				if len(got) != 0 {
					t.Fatalf("unexpected support: %+v", got)
				}
				return
			}
			if len(got) != 1 || got[0].State != tc.state {
				t.Fatalf("support=%+v want=%s", got, tc.state)
			}
			if tc.state == runnerapi.Deprecated {
				if got[0].Next == nil || got[0].Next.State != runnerapi.Unsupported || got[0].Next.On.String() != "2026-12-08" {
					t.Fatalf("next support transition = %+v", got[0].Next)
				}
				got[0].Next.State = runnerapi.Supported
				if again := k.Support(w.Views["test"], tc.capability); again[0].Next.State != runnerapi.Unsupported {
					t.Fatalf("next transition was mutable: %+v", again[0].Next)
				}
			}
		})
	}
}

func inventoryEntries(candidates []runnerapi.Candidate) string {
	var s strings.Builder
	for i, c := range candidates {
		if i > 0 {
			s.WriteByte(',')
		}
		fmt.Fprintf(&s, `{"name":"%d","labels":["queue"],"hosting":%q,"os":%q,"architecture":%q}`, i, c.Hosting, c.OS, c.Architecture)
	}
	return s.String()
}

func TestExternalProviderUsesOnlyItsSupportCatalog(t *testing.T) {
	t.Parallel()
	w := runnerWorkflow(t, "queue")
	answer := runnerapi.Answer{Resolution: runnerapi.Resolved{Candidates: []runnerapi.Candidate{{Name: "runner", Hosting: runnerapi.SelfHosted, Architecture: "arm"}}}}
	provider := &supportProvider{response: runnerapi.Response{Answers: []runnerapi.Answer{answer}}}
	opts := runnerapi.Options{Provider: "custom", Providers: []runnerapi.Registration{{Provider: provider}}, Service: service(t, "github.com", ""), AsOf: date(t, "2026-12-08")}
	k, err := Prepare(t.Context(), w.Snapshot, nil, opts, "scope")
	if err != nil {
		t.Fatal(err)
	}
	if got := k.Support(w.Views["test"], runnerapi.JobExecution); len(got) != 0 {
		t.Fatalf("built-in policy leaked into external provider: %+v", got)
	}
	provider.response.Support = runnerapi.SupportCatalog{DatasetStatus: runnerapi.DatasetStatus{DatasetID: "custom-policy", Freshness: runnerapi.Fresh, Complete: true}, Policies: []runnerapi.SupportPolicy{{ID: "custom-arm", Description: "custom ARM runners", URL: "https://example.com/support", Capability: runnerapi.JobExecution, Hosting: []runnerapi.Hosting{runnerapi.SelfHosted}, Architectures: []string{"arm32"}, Initial: runnerapi.Unsupported}}}
	k, err = Prepare(t.Context(), w.Snapshot, nil, opts, "scope")
	if err != nil {
		t.Fatal(err)
	}
	got := k.Support(w.Views["test"], runnerapi.JobExecution)
	if len(got) != 1 || got[0].Policy.ID != "custom-arm" {
		t.Fatalf("external policy missing: %+v", got)
	}
	provider.response.Support.DatasetStatus.Freshness = runnerapi.Stale
	k, err = Prepare(t.Context(), w.Snapshot, nil, opts, "scope")
	if err != nil {
		t.Fatal(err)
	}
	if got := k.Support(w.Views["test"], runnerapi.JobExecution); len(got) != 0 {
		t.Fatalf("stale policy produced finding: %+v", got)
	}
}

func TestSnapshotSupportOverridesBuiltinAndRespectsFreshness(t *testing.T) {
	t.Parallel()
	w := runnerWorkflow(t, "queue")
	content := `{"runners":[{"name":"one","labels":["queue"],"hosting":"self-hosted","architecture":"arm"}],"support":{"dataset_status":{"dataset_id":"local","scope":"scope","freshness":"fresh","complete":true},"policies":[]}}`
	opts := runnerapi.Options{Provider: "snapshot", Service: service(t, "github.com", ""), AsOf: date(t, "2026-12-08"), Snapshot: runnerapi.Snapshot{Content: content, Scope: "scope", Freshness: runnerapi.Fresh, Complete: true}}
	k, err := Prepare(t.Context(), w.Snapshot, nil, opts, "scope")
	if err != nil {
		t.Fatal(err)
	}
	if got := k.Support(w.Views["test"], runnerapi.JobExecution); len(got) != 0 {
		t.Fatalf("explicit empty support did not replace builtin: %+v", got)
	}
	if report := k.Report(); report.Support.DatasetStatus.DatasetID != "local" {
		t.Fatalf("support provenance lost: %+v", report.Support)
	}
	staleCatalog := strings.Replace(content, `"freshness":"fresh"`, `"freshness":"stale"`, 1)
	opts.Snapshot.Content = staleCatalog
	k, err = Prepare(t.Context(), w.Snapshot, nil, opts, "scope")
	if err != nil {
		t.Fatal(err)
	}
	if k.Report().Support.DatasetStatus.Freshness != runnerapi.Stale {
		t.Fatalf("stale catalogue promoted to fresh: %+v", k.Report().Support)
	}
	opts.Snapshot.Content = content
	opts.Snapshot.Freshness = runnerapi.Stale
	k, err = Prepare(t.Context(), w.Snapshot, nil, opts, "scope")
	if err != nil {
		t.Fatal(err)
	}
	if got := k.Support(w.Views["test"], runnerapi.JobExecution); len(got) != 0 {
		t.Fatalf("stale snapshot made support claim: %+v", got)
	}
}
