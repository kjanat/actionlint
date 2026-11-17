package workflowvisit

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

type jobOrderPass struct {
	Base
	jobs []string
}

func (p *jobOrderPass) VisitJobPre(job declaration.Job) error {
	p.jobs = append(p.jobs, job.Key())
	return nil
}

func TestVisitorJobSourceOrder(t *testing.T) {
	t.Parallel()
	first := &workflowlang.Job{Pos: &source.Position{Line: 1, Column: 1}}
	second := &workflowlang.Job{Pos: &source.Position{Line: 2, Column: 1}}
	missingA, missingZ := &workflowlang.Job{}, &workflowlang.Job{}
	for range 10 {
		pass := &jobOrderPass{}
		workflow := declaration.Bind("order", "", &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"z": first, "b": second, "missing-a": missingA, "missing-z": missingZ}}, false, "").Workflow()
		if err := Visit(workflow, Options{Passes: []Pass{pass}}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(pass.jobs, []string{"z", "b", "missing-a", "missing-z"}) {
			t.Fatalf("unexpected job order: %v", pass.jobs)
		}
	}
}
