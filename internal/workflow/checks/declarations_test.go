package checks

import (
	"reflect"
	"slices"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
)

func TestDeclarationsAreIndependentOfTraversalOrder(t *testing.T) {
	const src = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - id: same\n        run: echo first\n      - id: same\n        run: echo second\n"
	ast, errs := workflowparser.Parse([]byte(src))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	w := declaration.Publish(src, ast, false, "").Bind("test").Workflow()
	check := Declaration("id")
	want := check.Job(jobView(w, "build"))
	if len(want) != 1 || want[0].Step.Reference() != slices.Collect(jobView(w, "build").Steps())[1].Reference() {
		t.Fatalf("missing duplicate owner: %+v", want)
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			check.Step(slices.Collect(jobView(w, "build").Steps())[1])
			check.Workflow(w)
			if got := check.Job(jobView(w, "build")); !reflect.DeepEqual(got, want) {
				t.Errorf("result depends on callback order: %+v", got)
			}
		})
	}
	workers.Wait()
}

func TestEnvironmentNameFindingsUseKeyOrigins(t *testing.T) {
	ast, errs := workflowparser.Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    env:\n      FOO=BAR: first\n      FOO BAR: second\n    steps: [{run: echo ok}]\n"))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	w := declaration.Publish("", ast, false, "").Bind("test").Workflow()
	findings := Declaration("env-var").Job(jobView(w, "test"))
	if len(findings) != 2 {
		t.Fatalf("want two invalid names: %v", findings)
	}
	seen := map[int]bool{}
	for _, finding := range findings {
		if finding.Start.Line < 6 || finding.Start.Line > 7 || finding.Start.Column != 7 || seen[finding.Start.Line] {
			t.Fatalf("name diagnostic points at value: %+v", finding)
		}
		seen[finding.Start.Line] = true
	}
}

func TestParallelJobsDoNotShareBackgroundSteps(t *testing.T) {
	ast, errs := workflowparser.Parse([]byte(`on: push
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
	w := declaration.Publish("", ast, false, "").Bind("test").Workflow()
	want := ParallelSteps(jobView(w, "consumer"))
	if len(want) != 1 || want[0].Step.Reference() != slices.Collect(jobView(w, "consumer").Steps())[0].Reference() {
		t.Fatalf("expected unknown background step with its owner: %+v", want)
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			if got := ParallelSteps(jobView(w, "producer")); len(got) != 0 {
				t.Errorf("valid job: %+v", got)
			}
			if got := ParallelSteps(jobView(w, "consumer")); !reflect.DeepEqual(got, want) {
				t.Errorf("another job changed the result: %+v", got)
			}
		})
	}
	workers.Wait()
}

func TestRequiredActionsDoNotRetainEarlierWorkflow(t *testing.T) {
	parse := func(src string) declaration.WorkflowView {
		t.Helper()
		ast, errs := workflowparser.Parse([]byte(src))
		if len(errs) != 0 {
			t.Fatal(errs)
		}
		return declaration.Publish(src, ast, false, "").Bind("test").Workflow()
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

func jobView(w declaration.WorkflowView, key string) declaration.JobView {
	job, _ := w.Job(key)
	return job
}
