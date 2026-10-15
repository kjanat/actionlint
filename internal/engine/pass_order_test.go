package engine

import (
	"reflect"
	"testing"
)

type jobOrderPass struct {
	RuleBase
	jobs []*Job
}

func (p *jobOrderPass) VisitJobPre(job *Job) error {
	p.jobs = append(p.jobs, job)
	return nil
}

func TestVisitorJobSourceOrder(t *testing.T) {
	first := &Job{Pos: &Pos{Line: 1, Col: 1}}
	second := &Job{Pos: &Pos{Line: 2, Col: 1}}
	missingA, missingZ := &Job{}, &Job{}
	for range 10 {
		pass := &jobOrderPass{}
		visitor := NewVisitor()
		visitor.AddPass(pass)
		if err := visitor.Visit(&Workflow{Jobs: map[string]*Job{"z": first, "b": second, "missing-a": missingA, "missing-z": missingZ}}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(pass.jobs, []*Job{first, second, missingA, missingZ}) {
			t.Fatalf("unexpected job order: %v", pass.jobs)
		}
	}
}
