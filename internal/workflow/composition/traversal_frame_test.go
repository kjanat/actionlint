package composition

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/builtin"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

type frameObserver struct {
	workflowvisit.Base
	enter func(builtin.Invocation) error
	step  func() error
}

func (p *frameObserver) EnterInvocation(call builtin.Invocation) error { return p.enter(call) }
func (p *frameObserver) LeaveInvocation(invocation.Context) error      { return nil }
func (p *frameObserver) VisitStep(workflowdecl.StepView) error         { return p.step() }
func (p *frameObserver) VisitInvocationStep(step workflowdecl.StepView) error {
	return p.VisitStep(step)
}

func (p *frameObserver) VisitOpaqueAction(step workflowdecl.StepView) error {
	return p.VisitStep(step)
}

func TestCompositeTraversalDoesNotReplaceCallerFrame(t *testing.T) {
	failure := errors.New("child traversal failed")
	for _, childError := range []error{nil, failure} {
		name := "success"
		if childError != nil {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}]}}\n", map[string]string{
				"outer/action.yml": "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: $/inner}]}\n",
				"inner/action.yml": "name: inner\ndescription: inner\nruns: {using: composite, steps: [{shell: bash, run: echo inner}]}\n",
			}, nil)
			rootConfig := &repoconfig.Config{}
			f.options.Config = rootConfig
			f.options.ConfigForFile = func(string) (*repoconfig.Config, error) { return &repoconfig.Config{}, nil }
			entered, visited := 0, 0
			_, err := analyze(f.options, func(parent *compositeAnalyzer) error {
				if err := parent.startEntry(invocation.EntryWorkflow, f.root); err != nil {
					return err
				}
				workflow := workflowView(t, f.root)
				if err := parent.beginWorkflow(workflow); err != nil {
					return err
				}
				job := workflowJob(t, workflow, "test")
				if err := parent.beginJob(job); err != nil {
					return err
				}
				rootContext, rootScope := parent.context, parent.scriptScope
				assertCaller := func() {
					t.Helper()
					if parent.context != rootContext || parent.sourceSnapshot != f.root || parent.scriptScope != rootScope || parent.scriptConfig != rootConfig {
						t.Fatal("nested traversal replaced the live caller frame")
					}
				}
				probe := &frameObserver{Base: workflowvisit.NewBase("observe-frame", ""), enter: func(call builtin.Invocation) error {
					assertCaller()
					if call.Context == rootContext || call.Source == f.root {
						t.Fatal("child did not receive its own invocation, source and resolver")
					}
					entered++
					return nil
				}, step: func() error {
					assertCaller()
					visited++
					return childError
				}}
				err := parent.visitActionScripts(workflowStepViews(t, workflow, "test")[0], []builtin.InvocationParticipant{probe}, nil)
				assertCaller()
				return err
			})
			if !errors.Is(err, childError) || entered != 2 || visited != 1 {
				t.Fatalf("nested traversal: error=%v entered=%d visited=%d", err, entered, visited)
			}
		})
	}
}
