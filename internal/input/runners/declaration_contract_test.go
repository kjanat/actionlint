package runners

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	runnermodel "actionlint.kjanat.dev/v2/internal/model/runners"

	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
)

func TestPreparedDeclarationRejectionsPreserveExcludedSpelling(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, exclude, include string
		want                   int
	}{
		{"excluded original", `[{os: ubuntu-lates}]`, `"${{ fromJSON(vars.INCLUDE) }}"`, 1},
		{"excluded union", `[{os: ubuntu-lates, arch: x64}, {os: ubuntu-lates, arch: arm64}]`, `"${{ fromJSON(vars.INCLUDE) }}"`, 1},
		{"excluded original and separate include", `[{os: ubuntu-lates}]`, `[{os: ubuntu-lates}, "${{ fromJSON(vars.INCLUDE) }}"]`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix: {os: [ubuntu-latest, ubuntu-lates], arch: [x64, arm64], exclude: "+tc.exclude+", include: "+tc.include+"}\n    runs-on: '${{ matrix.os }}'\n    steps: [{run: echo ok}]\n")
			knowledge, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
			if err != nil {
				t.Fatal(err)
			}
			job := knowledge.Job(w.Views["test"])
			if len(job.Rejections) != tc.want || job.Platform != runnermodel.Any || job.Shell != "" || !reflect.DeepEqual(job.Rejections, runnermodel.LabelRejections(w.Views["test"], runnermodel.KnownLabels(nil))) {
				t.Fatalf("declaration diagnostics diverged or supplied execution certainty: %+v", job)
			}
			for _, finding := range job.Rejections {
				if !strings.Contains(finding.Reason, `label "ubuntu-lates" is unknown`) {
					t.Fatalf("unexpected declaration finding: %+v", finding)
				}
			}
		})
	}
}

func TestDeclarationChecksPreserveExternalProviderAuthority(t *testing.T) {
	t.Parallel()
	failure := errors.New("provider unavailable")
	for _, tc := range []struct {
		name           string
		resolution     runnerapi.Resolution
		failure        error
		wantRejections int
		wantShell      string
	}{
		{name: "custom labels resolve", resolution: runnerapi.Resolved{Candidates: []runnerapi.Candidate{{Name: "custom", OS: runnerapi.Linux, DefaultShell: "fish"}}}, wantShell: "fish"},
		{name: "unknown stays unknown", resolution: runnerapi.Unknown{Reason: "runtime choice"}},
		{name: "explicit invalid response", resolution: runnerapi.NoMatch{Rejections: []runnerapi.Rejection{{Reason: "provider rejected queue"}}}, wantRejections: 1},
		{name: "provider failure is not invalid", failure: failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix: {os: [ubuntu-lates, queue-windows], exclude: [{os: ubuntu-lates}]}\n    runs-on: '${{ matrix.os }}'\n    steps: [{run: echo ok}]\n")
			provider := &testProvider{resolve: func(request runnerapi.Request) ([]runnerapi.Answer, error) {
				answers := make([]runnerapi.Answer, len(request.Selectors))
				for i := range answers {
					answers[i] = runnerapi.Answer{Resolution: tc.resolution, DatasetStatus: runnerapi.DatasetStatus{DatasetID: "external", Scope: request.Scope, Freshness: runnerapi.Fresh, Complete: true}}
				}
				return answers, tc.failure
			}}
			knowledge, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{Target: "github", Provider: "queues", Providers: []runnerapi.Registration{{Provider: provider}}}, "scope")
			if tc.failure != nil && !errors.Is(err, tc.failure) || tc.failure == nil && err != nil {
				t.Fatalf("provider error changed: %v", err)
			}
			job := knowledge.Job(w.Views["test"])
			if len(job.Rejections) != tc.wantRejections || job.Shell != tc.wantShell {
				t.Fatalf("built-in declaration semantics bypassed provider: %+v", job)
			}
			if len(job.Rejections) != 0 && !strings.Contains(job.Rejections[0].Reason, "provider rejected queue") {
				t.Fatalf("built-in labels replaced provider rejection: %+v", job.Rejections)
			}
		})
	}
	w := runnerWorkflow(t, "ubuntu-lates")
	knowledge, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{Target: "custom", Provider: "missing"}, "scope")
	if err == nil || !strings.Contains(err.Error(), "no runner provider") || len(knowledge.Job(w.Views["test"]).Rejections) != 0 {
		t.Fatalf("missing provider became a label rejection: knowledge=%+v error=%v", knowledge, err)
	}
}

func TestBuiltinLabelRejectionsRespectSnapshotEvidence(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix: {os: [ubuntu-lates, windows-latest], exclude: [{os: ubuntu-lates}]}\n    runs-on: [linux, '${{ matrix.os }}']\n    steps: [{run: echo ok}]\n")
			knowledge, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{Snapshot: tc.snapshot}, "scope")
			if err != nil {
				t.Fatal(err)
			}
			job := knowledge.Job(w.Views["test"])
			if len(job.Rejections) != tc.want || job.PathsKnown || job.Platform != runnermodel.Any || job.Shell != "" {
				t.Fatalf("snapshot evidence changed label certainty policy: %+v", job)
			}
			if tc.want == 2 && (!strings.Contains(job.Rejections[0].Reason, `label "ubuntu-lates" is unknown`) || !strings.Contains(job.Rejections[1].Reason, "conflicts with label")) {
				t.Fatalf("fresh evidence lost declaration spelling or executed compatibility: %+v", job.Rejections)
			}
		})
	}
}
