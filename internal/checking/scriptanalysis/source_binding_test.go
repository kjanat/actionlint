package scriptanalysis

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

func actionBatchFixture(t *testing.T, mapped bool) (invocation.Graph, Collection, parsing.SourceSnapshot, string) {
	t.Helper()

	graph, collection, snapshot, path, _ := actionBatchFixtureWithDocument(t, mapped)
	return graph, collection, snapshot, path
}

func actionBatchFixtureWithDocument(t *testing.T, mapped bool) (invocation.Graph, Collection, parsing.SourceSnapshot, string, parsing.Document) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source", "action.yml")
	content := []byte("name: source\ndescription: source\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo first\n    - shell: sh\n      run: echo second\n")
	registry, _, snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	document := parsing.Action(path, content)
	snapshot, err := snapshots.Document(path, content, document)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := NewDeployment(registry, snapshot, path, path, mapped)
	if err != nil {
		t.Fatal(err)
	}
	builder, err := invocation.NewBuilder(invocation.EntryAction, snapshot.Reference(), invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	scope := &Scope{Context: builder.RootContext(), Deployment: deployment, Composite: true, Config: &configuration.Config{}}
	execution.ActionOrigin(&scope.Paths, scope.Context, deployment.ActionPath())
	collection := Collection{Declarations: registry, Scopes: []*Scope{scope}, Collected: true}
	body, available := snapshot.ActionView()
	steps := slices.Collect(body.Steps())
	if !available || len(steps) != 2 {
		t.Fatal("fixture action has no declarations")
	}
	for _, step := range steps {
		reference := step.Reference()
		id, err := builder.AddScript(builder.RootContext(), reference, invocation.ScriptRun, "")
		if err != nil {
			t.Fatal(err)
		}
		collection.Scripts = append(collection.Scripts, ScriptRequest{Occurrence: id, DefaultShell: new("pwsh")})
	}
	graph, err := builder.Publish()
	if err != nil {
		t.Fatal(err)
	}
	return graph, collection, snapshot, path, document
}

func TestDeploymentRejectsForeignSources(t *testing.T) {
	t.Parallel()
	_, actionCollection, action, path := actionBatchFixture(t, false)
	graph, workflow := batchFixture(t, false)
	root, _ := workflow.Declarations.Lookup(graph.Entry().Reference)
	for name, snapshot := range map[string]parsing.SourceSnapshot{"workflow": root, "zero": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewDeployment(actionCollection.Declarations, snapshot, path, path, false); !errors.Is(err, ErrInvalidBatch) {
				t.Fatalf("foreign deployment accepted: %v", err)
			}
		})
	}
	for _, wrong := range []string{"", filepath.Join(filepath.Dir(path), "other.yml")} {
		if _, err := NewDeployment(actionCollection.Declarations, action, wrong, wrong, false); !errors.Is(err, ErrInvalidBatch) {
			t.Fatalf("foreign display path accepted: %v", err)
		}
	}
	foreignGraph, foreign, _, _ := actionBatchFixture(t, false)
	deployment, err := NewDeployment(actionCollection.Declarations, action, path, path, false)
	if err != nil {
		t.Fatal(err)
	}
	foreign.Scopes[0].Deployment = deployment
	execution.ActionOrigin(&foreign.Scopes[0].Paths, foreign.Scopes[0].Context, deployment.ActionPath())
	if _, err := NewBatch(foreignGraph, foreign); !errors.Is(err, ErrInvalidBatch) {
		t.Fatalf("deployment from another action accepted: %v", err)
	}
}

func TestBatchRejectsUnindexedDeclarations(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"node", "address", "kind", "input"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			graph, collection := batchFixture(t, true)
			builder, err := invocation.NewBuilder(invocation.EntryWorkflow, graph.Entry().Reference, invocation.DefaultBudget())
			if err != nil {
				t.Fatal(err)
			}
			record := graph.Scripts()[0]
			request := ScriptRequest{}
			switch change {
			case "node":
				record.Declaration.Node = "unknown"
			case "address":
				record.Declaration.Address = "unknown"
			case "kind":
				record.Declaration = graph.Scripts()[1].Declaration
			case "input":
				record = graph.Scripts()[1]
				record.Input = "absent"
				request.Input = &checkinputs.ScriptInput{Action: "actions/github-script", Input: "absent", Language: "javascript"}
			}
			request.Occurrence, err = builder.AddScript(builder.RootContext(), record.Declaration, record.Kind, record.Input)
			if err != nil {
				t.Fatal(err)
			}
			collection.Scopes[0].Context = builder.RootContext()
			collection.Scripts = []ScriptRequest{request}
			graph, err = builder.Publish()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := NewBatch(graph, collection); !errors.Is(err, ErrInvalidBatch) {
				t.Fatalf("unindexed script accepted: %v", err)
			}
		})
	}
}

func TestBatchRetainsPartialScriptsAfterCancellation(t *testing.T) {
	t.Parallel()
	graph, collection := batchFixture(t, true)
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, graph.Entry().Reference, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	id, err := builder.AddScript(builder.RootContext(), graph.Scripts()[0].Declaration, invocation.ScriptRun, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.InterruptEntry(invocation.ReasonCanceled, "cancelled after first script"); err != nil {
		t.Fatal(err)
	}
	collection.Scopes[0].Context = builder.RootContext()
	collection.Scripts = []ScriptRequest{{Occurrence: id, DefaultShell: new("pwsh")}}
	graph, err = builder.Publish()
	if err != nil {
		t.Fatal(err)
	}
	batch, err := NewBatch(graph, collection)
	if err != nil || !batch.CallGraph.Incomplete() || len(batch.Scripts) != 1 {
		t.Fatalf("cancelled partial collection lost: %v", err)
	}
	if units := batch.Scripts; units[0].Shell == nil || *units[0].Shell != "pwsh" {
		t.Fatal("inherited default was lost for a declaration without shell")
	}
}

func TestActionEntryWithoutScriptsNeedsDeploymentOnly(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "action.yml")
	content := []byte("name: node\ndescription: node\nruns: {using: node24, main: index.js}\n")
	registry, _, fixture := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := fixture.Document(path, content, parsing.Action(path, content))
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := NewDeployment(registry, snapshot, path, path, false)
	if err != nil {
		t.Fatal(err)
	}
	builder, err := invocation.NewBuilder(invocation.EntryAction, snapshot.Reference(), invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	graph, err := builder.Publish()
	if err != nil {
		t.Fatal(err)
	}
	for _, collected := range []bool{false, true} {
		scope := &Scope{Context: builder.RootContext(), Deployment: deployment}
		execution.ActionOrigin(&scope.Paths, scope.Context, deployment.ActionPath())
		collection := Collection{Scopes: []*Scope{scope}, Collected: collected}
		batch, err := NewBatch(graph, collection)
		if err != nil || !batch.Valid() || len(batch.Scripts) != 0 || len(batch.Scopes) != 1 || batch.Scopes[0].Composite || batch.Collected != collected {
			t.Fatalf("empty noncomposite action batch (collected=%v): %v", collected, err)
		}
		scope.Deployment = Deployment{}
		execution.ActionOrigin(&scope.Paths, scope.Context, "")
		if _, err := NewBatch(graph, collection); !errors.Is(err, ErrInvalidBatch) {
			t.Fatalf("action entry without deployment accepted (collected=%v): %v", collected, err)
		}
	}
}

func TestDeploymentPreservesWindowsDisplaySeparators(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("Windows path separator equivalence")
	}
	_, collection, snapshot, path := actionBatchFixture(t, false)
	if !strings.Contains(path, `\`) {
		t.Fatal("fixture did not provide a Windows display path")
	}
	for _, display := range []string{path, filepath.ToSlash(path)} {
		deployment, err := NewDeployment(collection.Declarations, snapshot, path, display, false)
		if err != nil || deployment.SourcePath() != display || deployment.reference != snapshot.Reference() {
			t.Fatalf("display separator normalization changed source identity: %+v %v", deployment, err)
		}
	}
}

func TestDeploymentValidatesSymlinkIdentity(t *testing.T) {
	t.Parallel()
	_, _, snapshot, path := actionBatchFixture(t, false)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, snapshot.Content(), 0o644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias.yml")
	if err := os.Symlink(path, alias); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, os.ErrPermission) {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	registry, store, fixture := newCapturedSnapshots(t, sourceFixtureBase(t))
	content := snapshot.Content()
	snapshot, err := fixture.Document(path, content, parsing.Action(path, content))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Capture(alias, content, sourcetext.YAML); err != nil {
		t.Fatal(err)
	}
	deployment, err := NewDeployment(registry, snapshot, alias, alias, false)
	if err != nil || deployment.SourcePath() != alias || deployment.reference != snapshot.Reference() {
		t.Fatalf("stable symlink alias rejected: %+v %v", deployment, err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(filepath.Dir(path), "other.yml")
	if err := os.WriteFile(other, snapshot.Content(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, alias); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if deployment, err := NewDeployment(registry, snapshot, alias, alias, false); err != nil || deployment.reference != snapshot.Reference() || deployment.Address() != alias {
		t.Fatalf("retargeted alias changed its captured identity after sealing: %+v %v", deployment, err)
	}
	if _, err := NewDeployment(registry, snapshot, other, other, false); !errors.Is(err, ErrInvalidBatch) {
		t.Fatalf("uncaptured target address accepted: %v", err)
	}
}
