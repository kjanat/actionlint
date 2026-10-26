package execution

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/fsread"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowexecution "actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
)

func actionBatchFixture(t *testing.T, mapped bool) (invocation.Graph, Collection, *documentparse.SourceSnapshot, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source", "action.yml")
	content := []byte("name: source\ndescription: source\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo first\n    - shell: sh\n      run: echo second\n")
	var snapshots documentparse.SourceSnapshots
	snapshot, err := snapshots.Document(fsread.PhysicalPath(path), content, documentparse.Action(path, path, content))
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := NewDeployment(snapshot, path, mapped)
	if err != nil {
		t.Fatal(err)
	}
	builder, err := invocation.NewBuilder(invocation.EntryAction, snapshot.Reference(), invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	scope := &Scope{Context: builder.RootContext(), Deployment: deployment, Composite: true, Config: &repoconfig.Config{}}
	workflowexecution.ActionOrigin(&scope.Paths, scope.Context, deployment.ActionPath())
	collection := Collection{Declarations: &snapshots, Scopes: []*Scope{scope}, Collected: true}
	steps, available := snapshot.ActionSteps()
	if !available || len(steps) != 2 {
		t.Fatal("fixture action has no declarations")
	}
	for _, step := range steps {
		reference, err := snapshot.StepReference(step)
		if err != nil {
			t.Fatal(err)
		}
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
	return graph, collection, snapshot, path
}

func TestBatchBindsActionSourceAndOwnsCopies(t *testing.T) {
	for _, mapped := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "mapped"}[mapped], func(t *testing.T) {
			graph, collection, snapshot, path := actionBatchFixture(t, mapped)
			original := snapshot.Content()
			collection.Scripts[0], collection.Scripts[1] = collection.Scripts[1], collection.Scripts[0]
			batch, err := NewBatch(graph, collection)
			if err != nil {
				t.Fatal(err)
			}
			scopes, units := batch.Snapshot()
			assertBatchFactsMatchCanonicalDeclarations(t, batch, collection.Declarations)
			for i, text := range []string{"echo first", "echo second"} {
				unit := units[i]
				if unit.Run.Run().Value() != text || unit.Occurrence != graph.Scripts()[i].ID || unit.Path != path || string(unit.Content) != string(original) || unit.Config != collection.Scopes[0].Config || unit.Scope != scopes[0] || unit.Owner.Job != "" || unit.Origin.Job != "" {
					t.Fatalf("source binding changed action provenance: %+v", unit)
				}
				if unit.Shell == nil || *unit.Shell != []string{"bash", "sh"}[i] {
					t.Fatal("inherited shell replaced the declaration shell")
				}
				fact, exists := batch.Fact(i, false)
				if !exists || fact.Path != "" || fact.JobID != "" || fact.Invocation != nil {
					t.Fatalf("standalone fact changed entry semantics: %+v", fact)
				}
			}
			if &units[0].Content[0] != &units[1].Content[0] {
				t.Fatal("one snapshot duplicated a shared source buffer")
			}
			wantActionPath := filepath.Dir(path)
			if mapped {
				wantActionPath = ""
			}
			if scopes[0].Deployment.ActionPath() != wantActionPath || scopes[0].Paths.ActionPath != wantActionPath {
				t.Fatal("deployment lost mapped-source semantics")
			}
			before, _ := batch.Fact(0, false)
			units[0].Content[0] = 'X'
			units[0].Owner.Step.Line = 999
			units[0].Origin.StepPosition.Col = 999
			if string(snapshot.Content()) != string(original) {
				t.Fatal("returned source bytes mutated the registry")
			}
			steps, _ := snapshot.ActionSteps()
			run := steps[0].Exec.(*workflowlang.ExecRun)
			if run.Run.Value != "echo first" || steps[0].Pos.Line == 999 || steps[0].Pos.Col == 999 || run.Source.Spans[0].YamlLine == 999 {
				t.Fatal("returned declaration mutated the registry")
			}
			run.Run.Value = "changed original"
			*collection.Scripts[0].DefaultShell = "changed default"
			after, _ := batch.Fact(0, false)
			fresh := batch.Scripts()
			if !reflect.DeepEqual(before, after) || string(fresh[0].Content) != string(original) || fresh[0].Owner.Step.Line == 999 || fresh[0].Origin.StepPosition.Col == 999 {
				t.Fatal("mutable inputs changed batch data")
			}
		})
	}
}

func TestDeploymentRejectsForeignSources(t *testing.T) {
	_, _, action, path := actionBatchFixture(t, false)
	graph, workflow := batchFixture(t, false)
	root, _ := workflow.Declarations.Lookup(graph.Entry().Reference)
	for name, snapshot := range map[string]*documentparse.SourceSnapshot{"nil": nil, "workflow": root, "zero": {}} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewDeployment(snapshot, path, false); !errors.Is(err, ErrInvalidBatch) {
				t.Fatalf("foreign deployment accepted: %v", err)
			}
		})
	}
	for _, wrong := range []string{"", filepath.Join(filepath.Dir(path), "other.yml")} {
		if _, err := NewDeployment(action, wrong, false); !errors.Is(err, ErrInvalidBatch) {
			t.Fatalf("foreign display path accepted: %v", err)
		}
	}
	foreignGraph, foreign, _, _ := actionBatchFixture(t, false)
	deployment, err := NewDeployment(action, path, false)
	if err != nil {
		t.Fatal(err)
	}
	foreign.Scopes[0].Deployment = deployment
	workflowexecution.ActionOrigin(&foreign.Scopes[0].Paths, foreign.Scopes[0].Context, deployment.ActionPath())
	if _, err := NewBatch(foreignGraph, foreign); !errors.Is(err, ErrInvalidBatch) {
		t.Fatalf("deployment from another action accepted: %v", err)
	}
}

func TestBatchRejectsUnindexedDeclarations(t *testing.T) {
	for _, change := range []string{"node", "address", "kind", "input"} {
		t.Run(change, func(t *testing.T) {
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
				request.Input = &analyzer.ScriptInput{Action: "actions/github-script", Input: "absent", Language: "javascript"}
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
	if err != nil || !batch.Invocations().Incomplete() || len(batch.Scripts()) != 1 {
		t.Fatalf("cancelled partial collection lost: %v", err)
	}
	if units := batch.Scripts(); units[0].Shell == nil || *units[0].Shell != "pwsh" {
		t.Fatal("inherited default was lost for a declaration without shell")
	}
}

func TestActionEntryWithoutScriptsNeedsDeploymentOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "action.yml")
	content := []byte("name: node\ndescription: node\nruns: {using: node24, main: index.js}\n")
	var registry documentparse.SourceSnapshots
	snapshot, err := registry.Document(fsread.PhysicalPath(path), content, documentparse.Action(path, path, content))
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := NewDeployment(snapshot, path, false)
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
		workflowexecution.ActionOrigin(&scope.Paths, scope.Context, deployment.ActionPath())
		collection := Collection{Scopes: []*Scope{scope}, Collected: collected}
		batch, err := NewBatch(graph, collection)
		if err != nil || !batch.Valid() || len(batch.Scripts()) != 0 || len(batch.Scopes()) != 1 || batch.Scopes()[0].Composite || batch.CollectedScripts() != collected {
			t.Fatalf("empty noncomposite action batch (collected=%v): %v", collected, err)
		}
		scope.Deployment = Deployment{}
		workflowexecution.ActionOrigin(&scope.Paths, scope.Context, "")
		if _, err := NewBatch(graph, collection); !errors.Is(err, ErrInvalidBatch) {
			t.Fatalf("action entry without deployment accepted (collected=%v): %v", collected, err)
		}
	}
}

func TestDeploymentPreservesWindowsDisplaySeparators(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path separator equivalence")
	}
	_, _, snapshot, path := actionBatchFixture(t, false)
	if !strings.Contains(path, `\`) {
		t.Fatal("fixture did not provide a Windows display path")
	}
	for _, display := range []string{path, filepath.ToSlash(path)} {
		deployment, err := NewDeployment(snapshot, display, false)
		if err != nil || deployment.SourcePath() != display || deployment.reference != snapshot.Reference() {
			t.Fatalf("display separator normalization changed source identity: %+v %v", deployment, err)
		}
	}
}

func TestDeploymentValidatesSymlinkIdentity(t *testing.T) {
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
	deployment, err := NewDeployment(snapshot, alias, false)
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
	if _, err := NewDeployment(snapshot, alias, false); !errors.Is(err, ErrInvalidBatch) {
		t.Fatalf("retargeted source alias accepted: %v", err)
	}
}
