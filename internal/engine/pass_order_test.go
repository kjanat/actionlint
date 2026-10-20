package engine

import (
	"reflect"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

type jobOrderPass struct {
	workflowvisit.Base
	jobs []*workflowlang.Job
}

func (p *jobOrderPass) VisitJobPre(job *workflowlang.Job) error {
	p.jobs = append(p.jobs, job)
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
		if err := visitor.Visit(&workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"z": first, "b": second, "missing-a": missingA, "missing-z": missingZ}}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(pass.jobs, []*workflowlang.Job{first, second, missingA, missingZ}) {
			t.Fatalf("unexpected job order: %v", pass.jobs)
		}
	}
}
