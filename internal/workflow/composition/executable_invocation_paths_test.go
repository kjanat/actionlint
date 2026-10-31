package composition

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/builtin"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// executableRule is what the probe needs from the executable-bit rule: the
// invocation hooks, checkout placement and the current script paths.
type executableRule interface {
	builtin.InvocationParticipant
	builtin.CheckoutParticipant
	Paths() execution.Paths
}

type executablePathsProbe struct {
	executableRule
	t       *testing.T
	want    execution.Paths
	entered int
}

func (p *executablePathsProbe) checkPaths() {
	p.t.Helper()
	got := p.Paths()
	if got.Platform != p.want.Platform || got.Workspace != p.want.Workspace || got.Analysis != p.want.Analysis {
		p.t.Fatalf("invocation replaced job paths: got %+v, wanted %+v", got, p.want)
	}
}

func (p *executablePathsProbe) EnterInvocation(call builtin.Invocation) error {
	p.checkPaths()
	if err := p.executableRule.EnterInvocation(call); err != nil {
		return err
	}
	p.checkPaths()
	if p.Paths().ActionPath != call.ActionPath {
		p.t.Fatal("invocation did not apply its action origin")
	}
	p.entered++
	return nil
}

func (p *executablePathsProbe) LeaveInvocation(call invocation.Context) error {
	if err := p.executableRule.LeaveInvocation(call); err != nil {
		return err
	}
	p.checkPaths()
	return nil
}

func TestExecutableInvocationPreservesJobPaths(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}, {run: echo after}]}}\n", map[string]string{
		"outer/action.yml": "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: $/inner}]}\n",
		"inner/action.yml": "name: inner\ndescription: inner\nruns: {using: composite, steps: [{shell: bash, run: echo inside}]}\n",
	}, nil)
	reader := &fsread.Reader{}
	want := execution.Paths{Workspace: f.options.Paths.Workspace, Analysis: "job-working-directory", Platform: runnerlang.MacOrLinux}
	p := &executablePathsProbe{executableRule: builtin.NewRuleExecutableBit(builtin.Context{
		Context: t.Context(), ProjectRoot: want.Workspace, WorkingDir: want.Analysis, Reader: reader,
	}), t: t, want: want}
	f.options.Rules = []workflowvisit.Rule{p}
	_, err := Workflow(f.options, f.root)
	if err != nil || p.entered != 2 {
		t.Fatalf("error=%v, invocations=%d", err, p.entered)
	}
	p.checkPaths()
	if p.Paths().ActionPath != "" {
		t.Fatal("child action origin escaped into workflow")
	}
}
