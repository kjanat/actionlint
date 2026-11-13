package passes

import (
	"reflect"
	"testing"

	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

type jobOrderPass struct {
	workflowvisit.Base
	jobs []string
}

func (p *jobOrderPass) VisitJobPre(job declaration.JobView) error {
	p.jobs = append(p.jobs, job.Key())
	return nil
}

func TestVisitorJobSourceOrder(t *testing.T) {
	t.Parallel()
	first := &workflowlang.Job{Pos: &workflowlang.Pos{Line: 1, Col: 1}}
	second := &workflowlang.Job{Pos: &workflowlang.Pos{Line: 2, Col: 1}}
	missingA, missingZ := &workflowlang.Job{}, &workflowlang.Job{}
	for range 10 {
		pass := &jobOrderPass{}
		workflow := declaration.Publish("", &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"z": first, "b": second, "missing-a": missingA, "missing-z": missingZ}}, false, "").Bind("order").Workflow()
		if err := Visit(workflow, Options{Passes: []workflowvisit.Pass{pass}}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(pass.jobs, []string{"z", "b", "missing-a", "missing-z"}) {
			t.Fatalf("unexpected job order: %v", pass.jobs)
		}
	}
}
