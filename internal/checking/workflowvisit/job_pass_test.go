package workflowvisit

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/workflows"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

type jobPhasePass struct {
	Base
	events []string
}

func (p *jobPhasePass) VisitJobPre(job workflows.BoundJob) error {
	p.events = append(p.events, "pre "+job.Key())
	return nil
}

func (p *jobPhasePass) VisitJobPost(job workflows.BoundJob) error {
	p.events = append(p.events, "post "+job.Key())
	return nil
}

func TestVisitJobPassWrapsEveryJobPhase(t *testing.T) {
	t.Parallel()
	pass := &jobPhasePass{}
	workflow := workflows.Bind("phases", "", &definition.Workflow{Jobs: map[string]*definition.Job{"build": {}}}, false, "").Workflow()
	err := Visit(workflow, Options{
		Passes: []Pass{pass},
		Hooks: Hooks{JobPass: func(wrapped Pass, job workflows.BoundJob, visit func() error) error {
			if wrapped != Pass(pass) {
				t.Fatalf("hook received pass %v, want %v", wrapped, pass)
			}
			pass.events = append(pass.events, "enter "+job.Key())
			err := visit()
			pass.events = append(pass.events, "leave "+job.Key())
			return err
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"enter build", "pre build", "leave build", "enter build", "post build", "leave build"}
	if !reflect.DeepEqual(pass.events, want) {
		t.Fatalf("events = %v, want %v", pass.events, want)
	}
}
