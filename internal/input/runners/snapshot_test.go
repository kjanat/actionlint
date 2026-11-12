package runners

import (
	"context"
	"errors"
	"testing"

	runnermodel "actionlint.kjanat.dev/v2/internal/model/runners"

	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
)

const desktopInventory = `{"runners":[{"name":"desktop","labels":["self-hosted","desktop-pool","x64"],"group":"build","os":"windows","default_shell":"pwsh","case_sensitivity":"insensitive"},{"name":"server","labels":["self-hosted","server-pool","arm64"],"group":"build","os":"linux","default_shell":"bash"}]}`

func TestSnapshotKnowledge(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, selector, shell string
		platform              runnermodel.Platform
		freshness             runnerapi.Freshness
		complete              bool
		findings              int
	}{
		{"custom label", "desktop-pool", "pwsh", runnermodel.Windows, runnerapi.Fresh, true, 0},
		{"case insensitive labels", "[SELF-HOSTED, DESKTOP-POOL, X64]", "pwsh", runnermodel.Windows, runnerapi.Fresh, true, 0},
		{"group and label", "{group: build, labels: desktop-pool}", "pwsh", runnermodel.Windows, runnerapi.Fresh, true, 0},
		{"wrong group", "{group: other, labels: desktop-pool}", "", runnermodel.Any, runnerapi.Fresh, true, 1},
		{"labels on different runners", "[desktop-pool, arm64]", "", runnermodel.Any, runnerapi.Fresh, true, 1},
		{"unknown label", "absent", "", runnermodel.Any, runnerapi.Fresh, true, 1},
		{"no hosted fallback", "ubuntu-latest", "", runnermodel.Any, runnerapi.Fresh, true, 1},
		{"ambiguous runners", "self-hosted", "", runnermodel.Any, runnerapi.Fresh, true, 0},
		{"runtime label", "${{ inputs.runner }}", "", runnermodel.Any, runnerapi.Fresh, true, 0},
		{"stale match", "desktop-pool", "", runnermodel.Any, runnerapi.Stale, true, 0},
		{"stale absence", "absent", "", runnermodel.Any, runnerapi.Stale, true, 0},
		{"incomplete match", "desktop-pool", "", runnermodel.Any, runnerapi.Fresh, false, 0},
		{"incomplete absence", "absent", "", runnermodel.Any, runnerapi.Fresh, false, 0},
		{"missing", "desktop-pool", "", runnermodel.Any, runnerapi.Missing, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			workflow := runnerWorkflow(t, tc.selector)
			options := runnerapi.Options{Provider: "snapshot", Snapshot: runnerapi.Snapshot{
				Content: desktopInventory, Scope: "repo", Freshness: tc.freshness, Complete: tc.complete,
			}}
			knowledge, err := Prepare(t.Context(), workflow.Snapshot, nil, options, "repo")
			if err != nil {
				t.Fatal(err)
			}
			job := knowledge.Job(workflow.Views["test"])
			if job.Shell != tc.shell || job.Platform != tc.platform || len(job.Rejections) != tc.findings {
				t.Fatalf("knowledge = %+v; want shell %q, platform %v, findings %d", job, tc.shell, tc.platform, tc.findings)
			}
		})
	}
}

func TestSnapshotJoinsOSAndShellIndependently(t *testing.T) {
	t.Parallel()
	workflow := runnerWorkflow(t, "{group: build}")
	knowledge, err := Prepare(t.Context(), workflow.Snapshot, nil, runnerapi.Options{
		Provider: "snapshot", Snapshot: runnerapi.Snapshot{
			Scope: "repo", Freshness: runnerapi.Fresh, Complete: true,
			Content: `{"runners":[{"name":"modern","group":"build","os":"windows","default_shell":"pwsh"},{"name":"legacy","group":"build","os":"windows","default_shell":"powershell"}]}`,
		},
	}, "repo")
	if err != nil {
		t.Fatal(err)
	}
	job := knowledge.Job(workflow.Views["test"])
	if job.Platform != runnermodel.Windows || job.Shell != "" || len(job.Rejections) != 0 {
		t.Fatalf("disagreement about shell must not erase OS: %+v", job)
	}
}

func TestSnapshotSelectionAndIdentity(t *testing.T) {
	t.Parallel()
	workflow := runnerWorkflow(t, "desktop-pool")
	options := runnerapi.Options{Snapshot: runnerapi.Snapshot{Content: desktopInventory, Scope: "repo", Freshness: runnerapi.Fresh, Complete: true}}
	knowledge, err := Prepare(t.Context(), workflow.Snapshot, nil, options, "repo")
	if err != nil || knowledge.Report().Provider.Name != "github" || len(knowledge.Job(workflow.Views["test"]).Rejections) != 1 {
		t.Fatalf("snapshot silently selected provider: %+v, %v", knowledge, err)
	}
	options.Provider = "snapshot"
	knowledge, err = Prepare(t.Context(), workflow.Snapshot, nil, options, "repo")
	if err != nil {
		t.Fatal(err)
	}
	identity := knowledge.Report().CacheKey
	options.Snapshot.Content = `{"runners":[{"name":"desktop","labels":["desktop-pool"]}]}`
	knowledge, err = Prepare(t.Context(), workflow.Snapshot, nil, options, "repo")
	if err != nil {
		t.Fatal(err)
	}
	job := knowledge.Job(workflow.Views["test"])
	if knowledge.Report().CacheKey == identity || job.Platform != runnermodel.Any || job.Shell != "" || job.PathsKnown {
		t.Fatalf("missing facts or changed identity mishandled: %+v", knowledge)
	}
	options.Snapshot = runnerapi.Snapshot{}
	knowledge, err = Prepare(t.Context(), workflow.Snapshot, nil, options, "repo")
	if err != nil || len(knowledge.Job(workflow.Views["test"]).Rejections) != 0 {
		t.Fatalf("absent snapshot should give unknown: %+v, %v", knowledge, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := runnermodel.SnapshotProvider().ResolveRunners(ctx, runnerapi.Request{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
