package workflowvisit

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/workflows"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

type jobOrderPass struct {
	Base
	jobs []string
}

func (p *jobOrderPass) VisitJobPre(job workflows.Job) error {
	p.jobs = append(p.jobs, job.Key())
	return nil
}

func TestVisitorJobSourceOrder(t *testing.T) {
	t.Parallel()
	first := &definition.Job{Pos: &source.Position{Line: 1, Column: 1}}
	second := &definition.Job{Pos: &source.Position{Line: 2, Column: 1}}
	missingA, missingZ := &definition.Job{}, &definition.Job{}
	for range 10 {
		pass := &jobOrderPass{}
		workflow := workflows.Bind("order", "", &definition.Workflow{Jobs: map[string]*definition.Job{"z": first, "b": second, "missing-a": missingA, "missing-z": missingZ}}, false, "").Workflow()
		if err := Visit(workflow, Options{Passes: []Pass{pass}}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(pass.jobs, []string{"z", "b", "missing-a", "missing-z"}) {
			t.Fatalf("unexpected job order: %v", pass.jobs)
		}
	}
}
