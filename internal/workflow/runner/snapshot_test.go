package runner

import (
	"context"
	"errors"
	"testing"

	runnerapi "actionlint.kjanat.dev/v2/runner"
)

const desktopInventory = `{"runners":[{"name":"desktop","labels":["self-hosted","desktop-pool","x64"],"group":"build","os":"windows","default_shell":"pwsh","case_sensitivity":"insensitive"},{"name":"server","labels":["self-hosted","server-pool","arm64"],"group":"build","os":"linux","default_shell":"bash"}]}`

func TestSnapshotKnowledge(t *testing.T) {
	for _, tc := range []struct {
		name, selector, shell string
		platform              Platform
		freshness             runnerapi.Freshness
		complete              bool
		findings              int
	}{
		{"custom label", "desktop-pool", "pwsh", Windows, runnerapi.Fresh, true, 0},
		{"case insensitive labels", "[SELF-HOSTED, DESKTOP-POOL, X64]", "pwsh", Windows, runnerapi.Fresh, true, 0},
		{"group and label", "{group: build, labels: desktop-pool}", "pwsh", Windows, runnerapi.Fresh, true, 0},
		{"wrong group", "{group: other, labels: desktop-pool}", "", Any, runnerapi.Fresh, true, 1},
		{"labels on different runners", "[desktop-pool, arm64]", "", Any, runnerapi.Fresh, true, 1},
		{"unknown label", "absent", "", Any, runnerapi.Fresh, true, 1},
		{"no hosted fallback", "ubuntu-latest", "", Any, runnerapi.Fresh, true, 1},
		{"ambiguous runners", "self-hosted", "", Any, runnerapi.Fresh, true, 0},
		{"runtime label", "${{ inputs.runner }}", "", Any, runnerapi.Fresh, true, 0},
		{"stale match", "desktop-pool", "", Any, runnerapi.Stale, true, 0},
		{"stale absence", "absent", "", Any, runnerapi.Stale, true, 0},
		{"incomplete match", "desktop-pool", "", Any, runnerapi.Fresh, false, 0},
		{"incomplete absence", "absent", "", Any, runnerapi.Fresh, false, 0},
		{"missing", "desktop-pool", "", Any, runnerapi.Missing, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow := runnerWorkflow(t, tc.selector)
			options := runnerapi.Options{Provider: "snapshot", Snapshot: runnerapi.Snapshot{
				Content: desktopInventory, Scope: "repo", Freshness: tc.freshness, Complete: tc.complete,
			}}
			knowledge, err := Prepare(t.Context(), workflow.Snapshot, nil, options, "repo")
			if err != nil {
				t.Fatal(err)
			}
			job := knowledge.Job(workflow.Views["test"])
			if job.Shell != tc.shell || job.Platform != tc.platform || len(job.Findings) != tc.findings {
				t.Fatalf("knowledge = %+v; want shell %q, platform %v, findings %d", job, tc.shell, tc.platform, tc.findings)
			}
		})
	}
}

func TestSnapshotInventoryValidation(t *testing.T) {
	for _, content := range []string{
		`null`, `{}`, `{"runners":null}`, `{"runners":[null]}`, `{"runners":[],"typo":true}`,
		`{"runners":[{"name":"a","shell":"pwsh"}]}`,
		`{"runners":[{"name":"a","os":"win"}]}`,
		`{"runners":[{"name":"a","case_sensitivity":"ignore"}]}`,
		`{"runners":[{"name":"a","labels":[""]}]}`,
		`{"runners":[{"name":"a"},{"name":"a"}]}`,
	} {
		_, err := (snapshotProvider{}).ResolveRunners(t.Context(), runnerapi.Request{
			Target: "github", Snapshot: runnerapi.Snapshot{Content: content},
		})
		if err == nil {
			t.Errorf("accepted invalid inventory: %s", content)
		}
	}
}

func TestSnapshotJoinsOSAndShellIndependently(t *testing.T) {
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
	if job.Platform != Windows || job.Shell != "" || len(job.Findings) != 0 {
		t.Fatalf("disagreement about shell must not erase OS: %+v", job)
	}
}

func TestSnapshotSelectionAndIdentity(t *testing.T) {
	workflow := runnerWorkflow(t, "desktop-pool")
	options := runnerapi.Options{Snapshot: runnerapi.Snapshot{Content: desktopInventory, Scope: "repo", Freshness: runnerapi.Fresh, Complete: true}}
	knowledge, err := Prepare(t.Context(), workflow.Snapshot, nil, options, "repo")
	if err != nil || knowledge.Report().Provider.Name != "github" || len(knowledge.Job(workflow.Views["test"]).Findings) != 1 {
		t.Fatalf("snapshot silently selected provider: %+v, %v", knowledge, err)
	}
	options.Provider = "snapshot"
	knowledge, err = Prepare(t.Context(), workflow.Snapshot, nil, options, "repo")
	if err != nil {
		t.Fatal(err)
	}
	identity := knowledge.Report().Identity
	options.Snapshot.Content = `{"runners":[{"name":"desktop","labels":["desktop-pool"]}]}`
	knowledge, err = Prepare(t.Context(), workflow.Snapshot, nil, options, "repo")
	if err != nil {
		t.Fatal(err)
	}
	job := knowledge.Job(workflow.Views["test"])
	if knowledge.Report().Identity == identity || job.Platform != Any || job.Shell != "" || job.PathsKnown {
		t.Fatalf("missing facts or changed identity mishandled: %+v", knowledge)
	}
	options.Snapshot = runnerapi.Snapshot{}
	knowledge, err = Prepare(t.Context(), workflow.Snapshot, nil, options, "repo")
	if err != nil || len(knowledge.Job(workflow.Views["test"]).Findings) != 0 {
		t.Fatalf("absent snapshot should give unknown: %+v, %v", knowledge, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := (snapshotProvider{}).ResolveRunners(ctx, runnerapi.Request{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
