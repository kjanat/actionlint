package checks

import (
	"reflect"
	"sync"
	"testing"

	"actionlint.kjanat.dev/internal/workflow"
)

func TestDeclarationsAreIndependentOfTraversalOrder(t *testing.T) {
	const src = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - id: same\n        run: echo first\n      - id: same\n        run: echo second\n"
	w, errs := workflow.Parse([]byte(src))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	check := Declaration("id")
	want := check.Job(w.Jobs["build"])
	if len(want) != 1 || want[0].Step != w.Jobs["build"].Steps[1] {
		t.Fatalf("missing duplicate owner: %+v", want)
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			check.Step(w.Jobs["build"].Steps[1])
			check.Workflow(w)
			if got := check.Job(w.Jobs["build"]); !reflect.DeepEqual(got, want) {
				t.Errorf("result depends on callback order: %+v", got)
			}
		})
	}
	workers.Wait()
}

func TestParallelJobsDoNotShareBackgroundSteps(t *testing.T) {
	w, errs := workflow.Parse([]byte(`on: push
jobs:
  producer:
    runs-on: ubuntu-latest
    steps:
      - id: worker
        background: true
        run: sleep 1
      - wait: worker
  consumer:
    runs-on: ubuntu-latest
    steps:
      - wait: worker
`))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	want := ParallelSteps(w.Jobs["consumer"])
	if len(want) != 1 || want[0].Step != w.Jobs["consumer"].Steps[0] {
		t.Fatalf("expected unknown background step with its owner: %+v", want)
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			if got := ParallelSteps(w.Jobs["producer"]); len(got) != 0 {
				t.Errorf("valid job: %+v", got)
			}
			if got := ParallelSteps(w.Jobs["consumer"]); !reflect.DeepEqual(got, want) {
				t.Errorf("another job changed the result: %+v", got)
			}
		})
	}
	workers.Wait()
}

func TestRequiredActionsDoNotRetainEarlierWorkflow(t *testing.T) {
	parse := func(src string) *workflow.Workflow {
		t.Helper()
		w, errs := workflow.Parse([]byte(src))
		if len(errs) != 0 {
			t.Fatal(errs)
		}
		return w
	}
	withAction := parse("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v5\n")
	withoutAction := parse("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n")
	required := []string{"actions/checkout"}
	for range 3 {
		if got := RequiredActions(withAction, required); len(got) != 0 {
			t.Fatalf("present action: %+v", got)
		}
		if got := RequiredActions(withoutAction, required); len(got) != 1 {
			t.Fatalf("missing action: %+v", got)
		}
	}
}
