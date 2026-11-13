package runners

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	runnermodel "actionlint.kjanat.dev/v2/internal/model/runners"

	"actionlint.kjanat.dev/v2/internal/model/parsing"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

type testProvider struct {
	request runnerapi.Request
	resolve func(runnerapi.Request) ([]runnerapi.Answer, error)
}

func (*testProvider) RunnerDescriptor() runnerapi.Descriptor {
	return runnerapi.Descriptor{Name: "queues", Version: "1", Targets: []string{"buildkite", "github"}}
}

func (p *testProvider) ResolveRunners(_ context.Context, r runnerapi.Request) ([]runnerapi.Answer, error) {
	p.request = r
	return p.resolve(r)
}

type runnerFixture struct {
	Jobs     map[string]*workflowlang.Job
	Snapshot parsing.SourceSnapshot
	Views    map[string]workflowdecl.JobView
}

func runnerWorkflow(t *testing.T, selection string) *runnerFixture {
	t.Helper()
	return runnerSource(t, "on: push\njobs:\n  test:\n    runs-on: "+selection+"\n    steps:\n      - run: echo ok\n")
}

func runnerSource(t *testing.T, source string) *runnerFixture {
	t.Helper()
	content := []byte(source)
	document := parsing.Workflow(content)
	if len(document.SyntaxErrors) != 0 {
		t.Fatal(document.SyntaxErrors)
	}
	registry := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := registry.Document("runner.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	workflow, available := snapshot.WorkflowView()
	if !available {
		t.Fatal("workflow snapshot is unavailable")
	}
	fixture := &runnerFixture{Jobs: document.Workflow.Jobs, Snapshot: snapshot, Views: map[string]workflowdecl.JobView{}}
	for id := range fixture.Jobs {
		view, ok := workflow.JobsAt(id)
		if !ok {
			t.Fatalf("job view %q is unavailable", id)
		}
		fixture.Views[id] = view
	}
	return fixture
}

func TestBuiltinCandidateOSIgnoresCustomLabelSpelling(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		selection string
		custom    []string
		want      runnerapi.OS
	}{
		{"[self-hosted, linux, macos-cache]", []string{"macos-cache"}, runnerapi.Linux},
		{"[self-hosted, macos-cache, linux]", []string{"macos-cache"}, runnerapi.Linux},
		{"[self-hosted, linux, xcode-build]", []string{"xcode-*"}, runnerapi.Linux},
		{"[self-hosted, linux, MACOS-cache]", []string{"MACOS-cache"}, runnerapi.Linux},
		{"[self-hosted, macos, linux-cache]", []string{"linux-cache"}, runnerapi.MacOS},
		{"macos-latest", nil, runnerapi.MacOS},
		{"xcode-27", nil, runnerapi.MacOS},
		{"xcode-cache", []string{"xcode-cache"}, runnerapi.UnknownOS},
		{"macos-cache", []string{"macos-cache"}, runnerapi.UnknownOS},
		{"ubuntu-cache", []string{"ubuntu-cache"}, runnerapi.UnknownOS},
		{"[self-hosted, xcode-cache]", []string{"xcode-*"}, runnerapi.UnknownOS},
	} {
		t.Run(tc.selection, func(t *testing.T) {
			t.Parallel()
			workflow := runnerWorkflow(t, tc.selection)
			knowledge, err := Prepare(t.Context(), workflow.Snapshot, tc.custom, runnerapi.Options{}, "scope")
			if err != nil || len(knowledge.Report().Answers) != 1 {
				t.Fatalf("knowledge=%+v err=%v", knowledge, err)
			}
			resolved, ok := knowledge.Report().Answers[0].Resolution.(runnerapi.Resolved)
			if !ok || len(resolved.Candidates) != 1 || resolved.Candidates[0].OS != tc.want {
				t.Fatalf("resolution=%+v, want OS=%s", knowledge.Report().Answers[0].Resolution, tc.want)
			}
			if tc.want == runnerapi.UnknownOS {
				candidate := resolved.Candidates[0]
				if candidate.DefaultShell != "" || candidate.CaseSensitivity != runnerapi.UnknownCase || candidate.ShIsDash || candidate.Ephemeral {
					t.Fatalf("custom label invented execution facts: %+v", candidate)
				}
			}
		})
	}
}

func TestBuiltinCustomWindowsLabelKnowledge(t *testing.T) {
	t.Parallel()
	for _, label := range []string{"self-32vcpu-windows-2022", "buildjet-8vcpu-windows-2022", "namespace-profile-8x16-windows", "my-WINDOWS-box"} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			workflow := runnerWorkflow(t, label)
			knowledge, err := Prepare(t.Context(), workflow.Snapshot, []string{label}, runnerapi.Options{}, "scope")
			if err != nil {
				t.Fatal(err)
			}
			job := knowledge.Job(workflow.Views["test"])
			if job.Platform != runnermodel.Windows || job.Shell != "pwsh" || !job.PathsKnown || !job.CaseInsensitive || len(job.Rejections) != 0 {
				t.Fatalf("configured Windows label lost execution facts: %#v", job)
			}
		})
	}
	for _, tc := range []struct {
		selection string
		custom    []string
		findings  int
	}{
		{"self-32vcpu-windows-2022", nil, 1},
		{"mywindows-box", []string{"mywindows-box"}, 0},
		{"my-windowsish-box", []string{"my-windowsish-box"}, 0},
		{"[linux, my-windows-box]", []string{"my-windows-box"}, 1},
		{"[my-windows-box, linux]", []string{"my-windows-box"}, 1},
		{"[macos, my-windows-box]", []string{"my-windows-box"}, 1},
	} {
		t.Run(tc.selection, func(t *testing.T) {
			t.Parallel()
			workflow := runnerWorkflow(t, tc.selection)
			knowledge, err := Prepare(t.Context(), workflow.Snapshot, tc.custom, runnerapi.Options{}, "scope")
			if err != nil {
				t.Fatal(err)
			}
			job := knowledge.Job(workflow.Views["test"])
			if job.Platform != runnermodel.Any || job.Shell != "" || job.PathsKnown || len(job.Rejections) != tc.findings {
				t.Fatalf("unproven or contradictory label inferred execution facts: %#v", job)
			}
		})
	}
}

func TestProviderKnowledgeUncertainty(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		resolution     runnerapi.Resolution
		fresh          runnerapi.Freshness
		complete       bool
		snapshot       runnerapi.Snapshot
		failure        error
		wantShell      string
		wantRejections int
	}{
		{"windows", runnerapi.Resolved{Candidates: []runnerapi.Candidate{{Name: "queue", OS: runnerapi.Windows, DefaultShell: "pwsh"}}}, runnerapi.Fresh, true, runnerapi.Snapshot{}, nil, "pwsh", 0},
		{"incomplete candidates", runnerapi.Resolved{Candidates: []runnerapi.Candidate{{Name: "queue", OS: runnerapi.Windows, DefaultShell: "pwsh"}}}, runnerapi.Fresh, false, runnerapi.Snapshot{}, nil, "", 0},
		{"no match", runnerapi.NoMatch{Rejections: []runnerapi.Rejection{{Reason: "no queue"}}}, runnerapi.Fresh, true, runnerapi.Snapshot{}, nil, "", 1},
		{"incomplete absence", runnerapi.NoMatch{Rejections: []runnerapi.Rejection{{Reason: "no queue"}}}, runnerapi.Fresh, false, runnerapi.Snapshot{}, nil, "", 0},
		{"stale absence", runnerapi.NoMatch{Rejections: []runnerapi.Rejection{{Reason: "no queue"}}}, runnerapi.Stale, true, runnerapi.Snapshot{}, nil, "", 0},
		{"stale host snapshot", runnerapi.Resolved{Candidates: []runnerapi.Candidate{{Name: "queue", OS: runnerapi.Windows, DefaultShell: "pwsh"}}}, runnerapi.Fresh, true, runnerapi.Snapshot{Content: "old", Freshness: runnerapi.Stale, Complete: true}, nil, "", 0},
		{"incomplete host snapshot", runnerapi.NoMatch{Rejections: []runnerapi.Rejection{{Reason: "no queue"}}}, runnerapi.Fresh, true, runnerapi.Snapshot{Content: "partial", Freshness: runnerapi.Fresh}, nil, "", 0},
		{"metadata-only host snapshot", runnerapi.NoMatch{Rejections: []runnerapi.Rejection{{Reason: "no queue"}}}, runnerapi.Fresh, true, runnerapi.Snapshot{ObtainedAt: "2026-10-19T00:00:00Z"}, nil, "", 0},
		{"unknown", runnerapi.Unknown{Reason: "runtime selector"}, runnerapi.Fresh, true, runnerapi.Snapshot{}, nil, "", 0},
		{"not applicable", runnerapi.NotApplicable{}, runnerapi.Fresh, true, runnerapi.Snapshot{}, nil, "", 0},
		{"failure", nil, runnerapi.Fresh, true, runnerapi.Snapshot{}, errors.New("offline"), "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := runnerWorkflow(t, "buildkite-windows")
			if tc.snapshot != (runnerapi.Snapshot{}) {
				tc.snapshot.Scope = "scope"
			}
			p := &testProvider{resolve: func(r runnerapi.Request) ([]runnerapi.Answer, error) {
				return []runnerapi.Answer{{Resolution: tc.resolution, DatasetStatus: runnerapi.DatasetStatus{DatasetID: "v1", Scope: r.Scope, Freshness: tc.fresh, Complete: tc.complete}}}, tc.failure
			}}
			k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{Target: "buildkite", Snapshot: tc.snapshot, Providers: []runnerapi.Registration{{Provider: p, ImplementationID: "artifact"}}}, "scope")
			if (err != nil) != (tc.failure != nil) {
				t.Fatal(err)
			}
			job := k.Job(w.Views["test"])
			if job.Shell != tc.wantShell || len(job.Rejections) != tc.wantRejections {
				t.Fatalf("knowledge = %#v", job)
			}
			if tc.wantShell == "" && job.Platform != runnermodel.Any {
				t.Fatalf("uncertain answer inferred platform: %v", job.Platform)
			}
		})
	}
}

func TestProviderRequestNormalizesSnapshotFreshness(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		snapshot runnerapi.Snapshot
		fresh    runnerapi.Freshness
		known    bool
	}{
		{"absent", runnerapi.Snapshot{}, runnerapi.Missing, true},
		{"metadata only", runnerapi.Snapshot{Scope: "scope", ObtainedAt: "2026-10-21T00:00:00Z"}, runnerapi.Missing, false},
		{"fresh", runnerapi.Snapshot{Scope: "scope", Freshness: runnerapi.Fresh, Complete: true}, runnerapi.Fresh, true},
		{"stale", runnerapi.Snapshot{Scope: "scope", Freshness: runnerapi.Stale, Complete: true}, runnerapi.Stale, false},
		{"missing", runnerapi.Snapshot{Scope: "scope", Freshness: runnerapi.Missing}, runnerapi.Missing, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := runnerWorkflow(t, "queue")
			p := &testProvider{resolve: func(r runnerapi.Request) ([]runnerapi.Answer, error) {
				if r.Snapshot.Freshness != tc.fresh {
					t.Errorf("request freshness = %q, want %q", r.Snapshot.Freshness, tc.fresh)
				}
				return []runnerapi.Answer{{Resolution: runnerapi.Resolved{Candidates: []runnerapi.Candidate{{Name: "bundled", OS: runnerapi.Windows, DefaultShell: "pwsh", CaseSensitivity: runnerapi.CaseInsensitive}}}, DatasetStatus: runnerapi.DatasetStatus{DatasetID: "bundled", Scope: r.Scope, Freshness: runnerapi.Fresh, Complete: true}}}, nil
			}}
			options := runnerapi.Options{Target: "buildkite", Snapshot: tc.snapshot, Providers: []runnerapi.Registration{{Provider: p}}}
			k, err := Prepare(t.Context(), w.Snapshot, nil, options, "scope")
			if err != nil {
				t.Fatal(err)
			}
			if got := k.Job(w.Views["test"]); got.PathsKnown != tc.known || (got.Shell == "pwsh") != tc.known {
				t.Fatalf("snapshot knowledge = %#v, want known=%t", got, tc.known)
			}
			if k.Report().SnapshotDigest != p.request.Snapshot.Digest() {
				t.Fatal("report does not identify the snapshot supplied to the provider")
			}
			if tc.snapshot != (runnerapi.Snapshot{}) {
				options.Snapshot.Freshness = tc.fresh
				normalized, err := Prepare(t.Context(), w.Snapshot, nil, options, "scope")
				if err != nil || normalized.Report().CacheKey != k.Report().CacheKey {
					t.Fatalf("equivalent snapshot freshness changed cache identity: %v", err)
				}
			}
		})
	}
}

func TestBuiltinKnowledgeWithoutHostSnapshot(t *testing.T) {
	t.Parallel()
	w := runnerWorkflow(t, "ubuntu-latest")
	k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
	if err != nil {
		t.Fatal(err)
	}
	job := k.Job(w.Views["test"])
	if !job.PathsKnown || job.Platform != runnermodel.MacOrLinux || job.Shell != "bash" || len(job.Answers) != 1 || job.Answers[0].DatasetStatus.Freshness != runnerapi.Fresh {
		t.Fatalf("absent host snapshot weakened bundled catalogue: %#v", job)
	}
}

func TestKnowledgeReadersOwnTheirValues(t *testing.T) {
	t.Parallel()
	w := runnerWorkflow(t, "ubuntu-lates")
	k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
	if err != nil {
		t.Fatal(err)
	}
	job, report := k.Job(w.Views["test"]), k.Report()
	if len(job.Rejections) != 1 || len(job.Answers) != 1 || len(report.Answers) != 1 || len(report.Provider.Targets) == 0 {
		t.Fatalf("fixture lacks rejected label evidence: job=%#v report=%#v", job, report)
	}
	wantJob, wantReport := k.Job(w.Views["test"]), k.Report()
	job.Rejections[0].Reason = "changed"
	job.Answers[0].Resolution.(runnerapi.NoMatch).Rejections[0].Reason = "changed"
	report.Answers[0].Resolution.(runnerapi.NoMatch).Rejections[0].Reason = "changed"
	report.Provider.Targets[0] = "changed"
	if !reflect.DeepEqual(k.Job(w.Views["test"]), wantJob) || !reflect.DeepEqual(k.Report(), wantReport) {
		t.Fatal("a reader's edit reached prepared runner knowledge")
	}
}

func TestProviderEmptyNoMatch(t *testing.T) {
	t.Parallel()
	for _, freshness := range []runnerapi.Freshness{runnerapi.Fresh, runnerapi.Stale, runnerapi.Missing} {
		for _, complete := range []bool{false, true} {
			t.Run(string(freshness)+"/complete="+strconv.FormatBool(complete), func(t *testing.T) {
				t.Parallel()
				w := runnerWorkflow(t, "queue")
				p := &testProvider{resolve: func(r runnerapi.Request) ([]runnerapi.Answer, error) {
					return []runnerapi.Answer{{Resolution: runnerapi.NoMatch{}, DatasetStatus: runnerapi.DatasetStatus{DatasetID: "v1", Scope: r.Scope, Freshness: freshness, Complete: complete}}}, nil
				}}
				k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{Target: "buildkite", Providers: []runnerapi.Registration{{Provider: p}}}, "scope")
				if freshness == runnerapi.Fresh {
					if err == nil || !strings.Contains(err.Error(), "no rejections") {
						t.Fatalf("empty no-match must fail provider validation: %v", err)
					}
				} else if err != nil || len(k.Job(w.Views["test"]).Rejections) != 0 {
					t.Fatalf("uncertain no-match became a definitive rejection: %v %#v", err, k.Job(w.Views["test"]))
				}
			})
		}
	}
}

func TestProviderSelectionAndEvidenceIdentity(t *testing.T) {
	t.Parallel()
	w := runnerWorkflow(t, "ubuntu-latest")
	dataset := "v1"
	p := &testProvider{resolve: func(r runnerapi.Request) ([]runnerapi.Answer, error) {
		return []runnerapi.Answer{{Resolution: runnerapi.Unknown{Reason: "unknown"}, DatasetStatus: runnerapi.DatasetStatus{DatasetID: dataset, Scope: r.Scope, Freshness: runnerapi.Fresh}}}, nil
	}}
	options := runnerapi.Options{Providers: []runnerapi.Registration{{Provider: p, ImplementationID: "first"}}}
	k, err := Prepare(t.Context(), w.Snapshot, nil, options, "scope")
	if err != nil || k.Report().Provider.Name != "github" || p.request.Target != "" {
		t.Fatalf("loading a provider changed authority: %v %#v", err, k.Report())
	}
	options.Target = "buildkite"
	first, err := Prepare(t.Context(), w.Snapshot, nil, options, "scope")
	if err != nil {
		t.Fatal(err)
	}
	options.Providers[0].ImplementationID = "second"
	second, err := Prepare(t.Context(), w.Snapshot, nil, options, "scope")
	if err != nil || first.Report().CacheKey == second.Report().CacheKey {
		t.Fatal("artifact identity not tracked", err)
	}
	options.Snapshot.Content = "updated"
	options.Snapshot.Scope = "scope"
	third, err := Prepare(t.Context(), w.Snapshot, nil, options, "scope")
	if err != nil || second.Report().CacheKey == third.Report().CacheKey {
		t.Fatal("snapshot identity not tracked", err)
	}
	dataset = "v2"
	fourth, err := Prepare(t.Context(), w.Snapshot, nil, options, "scope")
	if err != nil || third.Report().CacheKey == fourth.Report().CacheKey {
		t.Fatal("dataset identity not tracked", err)
	}
	options.Providers = append(options.Providers, options.Providers[0])
	if _, err := Prepare(t.Context(), w.Snapshot, nil, options, "scope"); err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatal(err)
	}
	options.Providers = options.Providers[:1]
	options.Snapshot.Scope = "other"
	if _, err := Prepare(t.Context(), w.Snapshot, nil, options, "scope"); err == nil {
		t.Fatal("accepted unrelated snapshot scope")
	}
	options.Snapshot.Scope = ""
	if _, err := Prepare(t.Context(), w.Snapshot, nil, options, "scope"); err == nil {
		t.Fatal("accepted supplied snapshot without requested scope")
	}
	options.Snapshot.Scope = "scope"
	options.Snapshot.Freshness = "frehs"
	if _, err := Prepare(t.Context(), w.Snapshot, nil, options, "scope"); err == nil {
		t.Fatal("accepted invalid freshness")
	}
}

func TestPreparedRunnerGroupUncertainty(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, label, group, shell string
		findings                  int
	}{
		{"literal", "windows-latest", "prod", "pwsh", 0},
		{"constant expression", "windows-latest", "${{ 'prod' }}", "pwsh", 0},
		{"dynamic", "windows-latest", "${{ vars.GROUP }}", "", 0},
		{"array", "windows-latest", `${{ fromJSON('["prod"]') }}`, "", 0},
		{"null", "windows-latest", "${{ null }}", "", 0},
		{"invalid label", "invalid-runner", "${{ vars.GROUP }}", "", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := runnerWorkflow(t, "\n      labels: "+tc.label+"\n      group: "+tc.group)
			k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
			if err != nil {
				t.Fatal(err)
			}
			job := k.Job(w.Views["test"])
			if job.Shell != tc.shell || len(job.Rejections) != tc.findings {
				t.Fatalf("group knowledge = %#v", job)
			}
			if tc.shell == "" && job.Platform != runnermodel.Any {
				t.Fatalf("unresolved group inferred platform: %v", job.Platform)
			}
		})
	}
}
