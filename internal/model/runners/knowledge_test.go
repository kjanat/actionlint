package runner

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/parsing"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

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

func TestProviderSelectorsPreserveGroupAndMatrix(t *testing.T) {
	w := runnerSource(t, `on: push
jobs:
  test:
    strategy:
      matrix:
        include:
          - {runner: {labels: [buildkite-windows, x64], group: windows}}
          - {runner: {labels: [buildkite-linux, arm64], group: linux}}
    runs-on: ${{ matrix.runner }}
    steps: [{run: echo ok}]
`)
	selectors := Selectors(w.Views["test"])
	if len(selectors) != 2 {
		t.Fatalf("%#v", selectors)
	}
	for i, expected := range [][]string{{"buildkite-windows", "x64", "windows"}, {"buildkite-linux", "arm64", "linux"}} {
		var got []string
		for _, c := range selectors[i].Labels {
			if v, ok := c.Value.(runnerapi.Literal); ok {
				got = append(got, string(v))
			}
		}
		if selectors[i].Group != nil {
			if v, ok := selectors[i].Group.Value.(runnerapi.Literal); ok {
				got = append(got, string(v))
			}
		}
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("selector %d = %v", i, got)
		}
	}
	constant := runnerWorkflow(t, `${{ fromJSON('{"labels":["buildkite-windows"],"group":"build"}') }}`)
	s := Selectors(constant.Views["test"])[0]
	if s.Expression != "" || s.Group == nil || len(s.Labels) != 1 {
		t.Fatalf("constant object unresolved: %#v", s)
	}
}
