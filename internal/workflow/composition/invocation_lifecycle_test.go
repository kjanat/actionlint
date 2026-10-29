package composition

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/builtin"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
	"actionlint.kjanat.dev/v2/source"
)

type invocationLifecycleProbe struct {
	workflowvisit.Base
	t            *testing.T
	frames       []builtin.Invocation
	events       *[]string
	enterFailure error
	stepFailure  error
	leaveFailure error
	cancel       context.CancelFunc
}

func (p *invocationLifecycleProbe) emit(phase string, ref source.Reference) {
	p.t.Helper()
	call := p.frames[len(p.frames)-1]
	*p.events = append(*p.events, p.Name()+":"+phase+":"+call.Path)
	before := len(p.Errs())
	p.Error(&workflowlang.Pos{Line: 1, Col: 1}, phase)
	p.t.Cleanup(func() {
		finding := p.Errs()[before]
		if finding.Path != call.Path || !bytes.Equal(finding.Source, call.Source.Content()) || finding.Reference == nil || *finding.Reference != ref {
			p.t.Errorf("%s finding lost invocation source: %+v", phase, finding)
		}
	})
}

func (p *invocationLifecycleProbe) EnterInvocation(call builtin.Invocation) error {
	p.frames = append(p.frames, call)
	p.emit("enter", call.Source.Reference())
	if len(p.frames) == 2 && p.enterFailure != nil {
		p.frames = p.frames[:len(p.frames)-1]
		return p.enterFailure
	}
	return nil
}

func (p *invocationLifecycleProbe) LeaveInvocation(call invocation.Context) error {
	p.t.Helper()
	if len(p.frames) == 0 || p.frames[len(p.frames)-1].Context != call {
		p.t.Fatal("invocation exit does not match the innermost successful entry")
	}
	p.emit("leave", p.frames[len(p.frames)-1].Source.Reference())
	p.frames = p.frames[:len(p.frames)-1]
	return p.leaveFailure
}

func (p *invocationLifecycleProbe) VisitInvocationStep(step declaration.StepView) error {
	p.emit("step", step.Reference())
	if p.cancel != nil {
		p.cancel()
	}
	return p.stepFailure
}

func (p *invocationLifecycleProbe) VisitOpaqueAction(step declaration.StepView) error {
	p.emit("opaque", step.Reference())
	return p.stepFailure
}

func TestOpaqueInvocationCallbackBindsSource(t *testing.T) {
	failure := errors.New("opaque callback failed")
	for _, callbackError := range []error{nil, failure} {
		name := "success"
		if callbackError != nil {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}]}}\n", map[string]string{
				"outer/action.yml": "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: actions/opaque@v1}]}\n",
			}, nil)
			var events []string
			probe := &invocationLifecycleProbe{Base: workflowvisit.NewBase("opaque-probe", ""), t: t, events: &events, stepFailure: callbackError}
			f.service.options.Rules = []workflowvisit.Rule{probe}
			_, err := f.service.Workflow(f.root)
			if !errors.Is(err, callbackError) || len(probe.frames) != 0 {
				t.Fatalf("opaque callback error=%v, wanted=%v; frames=%d", err, callbackError, len(probe.frames))
			}
			path := filepath.Join(f.service.options.Paths.Workspace, "outer", "action.yml")
			want := []string{"opaque-probe:enter:" + path, "opaque-probe:opaque:" + path, "opaque-probe:leave:" + path}
			if !slices.Equal(events, want) {
				t.Fatalf("events=%v, wanted=%v", events, want)
			}
		})
	}
}

func TestInvocationLifecycleUnwindsAndBindsFindings(t *testing.T) {
	entryFailure := errors.New("entry rejected")
	stepFailure := errors.New("step rejected")
	leaveFailure := errors.New("exit failed")
	for _, phase := range []string{"success", "entry", "step", "cancel", "exit", "step and exit"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := newInvocationFixture(t, ctx, "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}]}}\n", map[string]string{
				"outer/action.yml": "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: $/inner}]}\n",
				"inner/action.yml": "name: inner\ndescription: inner\nruns: {using: composite, steps: [{shell: bash, run: echo one}, {shell: bash, run: echo two}]}\n",
			}, nil)
			var events []string
			a := &invocationLifecycleProbe{Base: workflowvisit.NewBase("a", ""), t: t, events: &events}
			b := &invocationLifecycleProbe{Base: workflowvisit.NewBase("b", ""), t: t, events: &events}
			var wantError error
			switch phase {
			case "entry":
				b.enterFailure, wantError = entryFailure, entryFailure
			case "step":
				a.stepFailure, wantError = stepFailure, stepFailure
			case "cancel":
				a.cancel, wantError = cancel, context.Canceled
			case "exit":
				b.leaveFailure, wantError = leaveFailure, leaveFailure
			case "step and exit":
				a.stepFailure, wantError = stepFailure, stepFailure
				b.leaveFailure = leaveFailure
			}
			f.service.options.Rules = []workflowvisit.Rule{a, b}
			_, err := f.service.Workflow(f.root)
			if !errors.Is(err, wantError) || len(a.frames) != 0 || len(b.frames) != 0 {
				t.Fatalf("error=%v, wanted=%v; remaining frames a=%d b=%d", err, wantError, len(a.frames), len(b.frames))
			}
			if b.leaveFailure != nil && !errors.Is(err, leaveFailure) {
				t.Fatalf("exit failure lost: %v", err)
			}
			outer := filepath.Join(f.service.options.Paths.Workspace, "outer", "action.yml")
			inner := filepath.Join(f.service.options.Paths.Workspace, "inner", "action.yml")
			want := []string{"a:enter:" + outer, "b:enter:" + outer, "a:enter:" + inner, "b:enter:" + inner}
			if phase != "entry" {
				want = append(want, "a:step:"+inner)
				if a.stepFailure == nil {
					want = append(want, "b:step:"+inner)
					if phase != "cancel" {
						want = append(want, "a:step:"+inner, "b:step:"+inner)
					}
				}
				want = append(want, "b:leave:"+inner)
			}
			want = append(want, "a:leave:"+inner, "b:leave:"+outer, "a:leave:"+outer)
			if !slices.Equal(events, want) {
				t.Fatalf("events=%v, wanted=%v", events, want)
			}
		})
	}
}
