package runner

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	runnerapi "actionlint.kjanat.dev/v2/runner"
)

func TestPreparedDeclarationFindingsRetainExcludedSpelling(t *testing.T) {
	for _, tc := range []struct {
		name, exclude, include string
		want                   int
	}{
		{"excluded original", `[{os: ubuntu-lates}]`, `"${{ fromJSON(vars.INCLUDE) }}"`, 1},
		{"excluded union", `[{os: ubuntu-lates, arch: x64}, {os: ubuntu-lates, arch: arm64}]`, `"${{ fromJSON(vars.INCLUDE) }}"`, 1},
		{"excluded original and separate include", `[{os: ubuntu-lates}]`, `[{os: ubuntu-lates}, "${{ fromJSON(vars.INCLUDE) }}"]`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix: {os: [ubuntu-latest, ubuntu-lates], arch: [x64, arm64], exclude: "+tc.exclude+", include: "+tc.include+"}\n    runs-on: '${{ matrix.os }}'\n    steps: [{run: echo ok}]\n")
			knowledge, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
			if err != nil {
				t.Fatal(err)
			}
			job := knowledge.Job(w.Views["test"])
			if len(job.Findings) != tc.want || job.Platform != Any || job.Shell != "" || !reflect.DeepEqual(job.Findings, Check(w.Views["test"], nil)) {
				t.Fatalf("declaration diagnostics diverged or supplied execution certainty: %+v", job)
			}
			for _, finding := range job.Findings {
				if !strings.Contains(finding.Message, `label "ubuntu-lates" is unknown`) {
					t.Fatalf("unexpected declaration finding: %+v", finding)
				}
			}
		})
	}
}

func TestDeclarationChecksPreserveExternalProviderAuthority(t *testing.T) {
	failure := errors.New("provider unavailable")
	for _, tc := range []struct {
		name         string
		resolution   runnerapi.Resolution
		failure      error
		wantFindings int
		wantShell    string
	}{
		{name: "custom labels resolve", resolution: runnerapi.Resolved{Candidates: []runnerapi.Candidate{{Name: "custom", OS: runnerapi.Linux, DefaultShell: "fish"}}}, wantShell: "fish"},
		{name: "unknown stays unknown", resolution: runnerapi.Unknown{Reason: "runtime choice"}},
		{name: "explicit invalid remains invalid", resolution: runnerapi.NoMatch{Rejections: []runnerapi.Rejection{{Reason: "provider rejected queue"}}}, wantFindings: 1},
		{name: "provider failure is not invalid", failure: failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix: {os: [ubuntu-lates, queue-windows], exclude: [{os: ubuntu-lates}]}\n    runs-on: '${{ matrix.os }}'\n    steps: [{run: echo ok}]\n")
			provider := &testProvider{resolve: func(request runnerapi.Request) ([]runnerapi.Answer, error) {
				answers := make([]runnerapi.Answer, len(request.Selectors))
				for i := range answers {
					answers[i] = runnerapi.Answer{Resolution: tc.resolution, Evidence: runnerapi.Evidence{Dataset: "external", Scope: request.Scope, Freshness: runnerapi.Fresh, Complete: true}}
				}
				return answers, tc.failure
			}}
			knowledge, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{Target: "github", Provider: "queues", Providers: []runnerapi.Registration{{Provider: provider}}}, "scope")
			if tc.failure != nil && !errors.Is(err, tc.failure) || tc.failure == nil && err != nil {
				t.Fatalf("provider error changed: %v", err)
			}
			job := knowledge.Job(w.Views["test"])
			if len(job.Findings) != tc.wantFindings || job.Shell != tc.wantShell {
				t.Fatalf("built-in declaration semantics bypassed provider: %+v", job)
			}
			if len(job.Findings) != 0 && !strings.Contains(job.Findings[0].Message, "provider rejected queue") {
				t.Fatalf("built-in labels replaced provider rejection: %+v", job.Findings)
			}
		})
	}
	w := runnerWorkflow(t, "ubuntu-lates")
	knowledge, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{Target: "custom", Provider: "missing"}, "scope")
	if err == nil || !strings.Contains(err.Error(), "no runner provider") || len(knowledge.Job(w.Views["test"]).Findings) != 0 {
		t.Fatalf("missing provider became a label rejection: knowledge=%+v error=%v", knowledge, err)
	}
}

func TestBuiltinLabelFindingsRespectSnapshotEvidence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		snapshot runnerapi.Snapshot
		want     int
	}{
		{name: "no snapshot uses fresh built-in catalog", want: 2},
		{name: "fresh complete", snapshot: runnerapi.Snapshot{Scope: "scope", Freshness: runnerapi.Fresh, Complete: true}, want: 2},
		{name: "stale", snapshot: runnerapi.Snapshot{Scope: "scope", Freshness: runnerapi.Stale, Complete: true}},
		{name: "incomplete", snapshot: runnerapi.Snapshot{Scope: "scope", Freshness: runnerapi.Fresh}},
		{name: "metadata only", snapshot: runnerapi.Snapshot{Scope: "scope", ObtainedAt: "2026-10-27T00:00:00Z"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix: {os: [ubuntu-lates, windows-latest], exclude: [{os: ubuntu-lates}]}\n    runs-on: [linux, '${{ matrix.os }}']\n    steps: [{run: echo ok}]\n")
			knowledge, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{Snapshot: tc.snapshot}, "scope")
			if err != nil {
				t.Fatal(err)
			}
			job := knowledge.Job(w.Views["test"])
			if len(job.Findings) != tc.want || job.PathsKnown || job.Platform != Any || job.Shell != "" {
				t.Fatalf("snapshot evidence changed label certainty policy: %+v", job)
			}
			if tc.want == 2 && (!strings.Contains(job.Findings[0].Message, `label "ubuntu-lates" is unknown`) || !strings.Contains(job.Findings[1].Message, "conflicts with label")) {
				t.Fatalf("fresh evidence lost declaration spelling or executed compatibility: %+v", job.Findings)
			}
		})
	}
}
