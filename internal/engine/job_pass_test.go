package engine

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/declaration"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

type jobPhasePass struct {
	workflowvisit.Base
	events []string
}

func (p *jobPhasePass) VisitJobPre(job declaration.JobView) error {
	p.events = append(p.events, "pre "+job.Key())
	return nil
}

func (p *jobPhasePass) VisitJobPost(job declaration.JobView) error {
	p.events = append(p.events, "post "+job.Key())
	return nil
}

func TestVisitJobPassWrapsEveryJobPhase(t *testing.T) {
	pass := &jobPhasePass{}
	workflow := declaration.Publish("", &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"build": {}}}, false, "").Bind("phases").Workflow()
	err := Visit(workflow, Options{
		Passes: []workflowvisit.Pass{pass},
		Hooks: Hooks{JobPass: func(wrapped workflowvisit.Pass, job declaration.JobView, visit func() error) error {
			if wrapped != workflowvisit.Pass(pass) {
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
