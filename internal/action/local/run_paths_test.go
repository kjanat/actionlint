package local

import (
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
	"actionlint.kjanat.dev/v2/source"
)

func TestCompositeActionPathCasing(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "local/Nested/lib.sh", "VALUE=42\n")
	wantDirectory, err := filepath.EvalSymlinks(filepath.Join(root, "local", "Nested"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, checkout, spec, want string }{
		{"root", "", "./LOCAL/nested", "local/Nested"},
		{"placed", "Source", "./source/LOCAL/nested", "Source/local/Nested"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actions := &Resolver{caseInsensitive: true}
			actions.restoreCheckout(execution.NewCheckoutPlacement(execution.Checkout{Directory: execution.Directory{Kind: execution.DirectoryKnown, Path: tc.checkout}, CaseInsensitive: true}, execution.CheckoutPlacement{}))
			paths := execution.Paths{Workspace: root, Analysis: root, Platform: runnerlang.Windows, Placements: actions.CheckoutState(), Reader: fsread.New(t.Context(), nil, nil)}
			content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: " + tc.spec + "\n")
			parsed := documentparse.Workflow("fixture.yml", content)
			snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
			snapshot, err := snapshots.Document("fixture.yml", content, parsed)
			if err != nil {
				t.Fatal(err)
			}
			view, err := snapshot.StepView(parsed.Workflow.Jobs["test"].Steps[0])
			if err != nil {
				t.Fatal(err)
			}
			facts, err := execution.CallFacts(view, paths.Platform)
			if err != nil {
				t.Fatal(err)
			}
			builder, err := invocation.NewBuilder(invocation.EntryWorkflow, source.Reference{Document: "fixture", Node: "entry"}, invocation.DefaultBudget())
			if err != nil {
				t.Fatal(err)
			}
			ticket, err := builder.Begin(builder.RootContext(), source.Reference{Document: "fixture", Node: "call"}, facts)
			if err != nil {
				t.Fatal(err)
			}
			context, admitted := ticket.Context()
			if !admitted {
				t.Fatal("fixture call was not admitted")
			}
			execution.ActionOrigin(&paths, context, filepath.Join(root, "local", "Nested"))
			if paths.ActionRunnerPath != tc.want {
				t.Fatalf("runner action path=%q, want %q", paths.ActionRunnerPath, tc.want)
			}
			directory := paths.Resolve(paths.WorkingDirectory(value.Template("${{ github.action_path }}", value.Origin{})))
			if directory.Kind != execution.DirectoryKnown || directory.Path != wantDirectory {
				t.Fatalf("action source directory unresolved: %+v", directory)
			}
		})
	}
}

func TestCheckoutPlacementUsesOwnedRunnerAndInputs(t *testing.T) {
	content := []byte("on: push\njobs:\n  test:\n    runs-on: windows-latest\n    steps:\n      - uses: actions/checkout@v4\n        with: {path: repo}\n      - uses: ./REPO/child\n")
	parsed := documentparse.Workflow("fixture.yml", content)
	if len(parsed.Findings) != 0 {
		t.Fatal(parsed.Findings)
	}
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("fixture.yml", content, parsed)
	if err != nil {
		t.Fatal(err)
	}
	workflow, present := snapshot.WorkflowView()
	if !present {
		t.Fatal("workflow view unavailable")
	}
	job, present := workflow.Job("test")
	if !present {
		t.Fatal("job view unavailable")
	}
	resolver := NewResolver(newCapturedActionCache(t, t.TempDir(), nil), nil)
	resolver.BeginJob(workflow.Env().Mapping(), workflow.Defaults().Run().Semantic(), job)
	steps := parsed.Workflow.Jobs["test"].Steps
	checkout, err := snapshot.StepView(steps[0])
	if err != nil {
		t.Fatal(err)
	}
	resolver.ObserveCheckout(checkout)
	placement := resolver.CheckoutState()
	if !placement.Present() || placement.Details().Directory.Kind != execution.DirectoryKnown || placement.Details().Directory.Path != "repo" || !placement.Details().CaseInsensitive {
		t.Fatalf("canonical checkout facts were not applied: %+v", placement)
	}
	local, ok := resolver.localSpec("./REPO/child")
	if !ok || local != "./child" {
		t.Fatalf("case insensitive local action mapping=%q known=%v", local, ok)
	}
}
