package engine

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/declaration"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
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
	first := &workflowlang.Job{Pos: &workflowlang.Pos{Line: 1, Col: 1}}
	second := &workflowlang.Job{Pos: &workflowlang.Pos{Line: 2, Col: 1}}
	missingA, missingZ := &workflowlang.Job{}, &workflowlang.Job{}
	for range 10 {
		pass := &jobOrderPass{}
		visitor := NewVisitor()
		visitor.AddPass(pass)
		if err := visitor.Visit(declaration.Publish("", &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"z": first, "b": second, "missing-a": missingA, "missing-z": missingZ}}, false, "").Bind("order").Workflow()); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(pass.jobs, []string{"z", "b", "missing-a", "missing-z"}) {
			t.Fatalf("unexpected job order: %v", pass.jobs)
		}
	}
}
