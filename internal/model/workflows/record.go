// Package declaration binds parsed workflow and action syntax to its source
// document. Consumers read the parsed tree directly; Workflow, Job and Step
// handles add the owning document's identity and semantic lookups.
package declaration

import (
	"iter"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

// Bound is one decoded document bound to a registry's document identity. Over
// the parsed tree it adds the source references derived from that identity and
// the source text, so views can name and locate their declarations. Bind takes
// ownership of the tree: callers must not modify it afterwards.
type Bound struct {
	text       string
	using      string
	workflow   *workflowlang.Workflow
	references workflowfacts.References
	steps      map[source.Reference]*workflowlang.Step
}

// Bind indexes workflow under identity. action selects action-manifest step
// addresses; using is the action's runs.using value, empty for workflows.
func Bind(identity, text string, workflow *workflowlang.Workflow, action bool, using string) *Bound {
	refs := workflowfacts.Index(identity, []byte(text), workflow, action)
	bound := &Bound{text: text, using: using, workflow: workflow, references: refs, steps: map[source.Reference]*workflowlang.Step{}}
	for step, ref := range refs.Steps {
		bound.steps[ref] = step
	}
	return bound
}

func (b *Bound) Reference() source.Reference { return b.references.Document }
func (b *Bound) Workflow() Workflow {
	if b == nil || b.workflow == nil {
		return Workflow{}
	}
	return Workflow{b.workflow, b}
}

func (b *Bound) Step(reference source.Reference) (Step, bool) {
	step, ok := b.steps[reference]
	if !ok {
		return Step{}, false
	}
	return Step{step, b}, true
}

func (b *Bound) Owns(step Step) bool          { return step.Present() && b == step.owner }
func (b *Bound) OwnsWorkflow(w Workflow) bool { return w.Present() && b == w.owner }
func (b *Bound) OwnsJob(job Job) bool         { return job.Present() && b == job.owner }

// SourceIndex returns detached source identities and positions. A nil Bound
// returns the zero index.
func (b *Bound) SourceIndex() workflowfacts.SourceIndex {
	if b == nil {
		return workflowfacts.SourceIndex{}
	}
	return b.references.SourceIndex()
}

// ActionBody exposes composite action steps without a workflow lifecycle.
func (b *Bound) ActionBody() ActionBody { return ActionBody{workflow: b.Workflow()} }

// Workflow is a parsed workflow bound to its document.
type Workflow struct {
	*workflowlang.Workflow
	owner *Bound
}

func (w Workflow) Present() bool { return w.Workflow != nil }

func (w Workflow) Job(key string) (Job, bool) {
	if !w.Present() {
		return Job{}, false
	}
	job, ok := w.Workflow.Jobs[key]
	if !ok || job == nil {
		return Job{}, false
	}
	return Job{job, w.owner, key}, true
}

// Jobs yields the workflow's jobs keyed by lowercase ID.
func (w Workflow) Jobs() iter.Seq2[string, Job] {
	return func(yield func(string, Job) bool) {
		if !w.Present() {
			return
		}
		for key, job := range w.Workflow.Jobs {
			if job != nil && !yield(key, Job{job, w.owner, key}) {
				return
			}
		}
	}
}

// ScalarSource maps a decoded scalar back to its captured YAML. It is
// available for indexed expressions, conditions and dependency references.
func (w Workflow) ScalarSource(s *workflowlang.String) *workflowlang.ScriptSource {
	pos, ok := s.Position()
	if !ok || !w.Present() {
		return nil
	}
	return w.ExpressionSources[pos]
}

// Job is a parsed job bound to its document.
type Job struct {
	*workflowlang.Job
	owner *Bound
	key   string
}

func (j Job) Present() bool { return j.Job != nil }

// Key returns the lowercase job ID that indexes the job in the workflow.
func (j Job) Key() string { return j.key }

func (j Job) Reference() source.Reference {
	if !j.Present() || j.owner == nil {
		return source.Reference{}
	}
	return j.owner.references.Jobs[j.Key()]
}

func (j Job) Workflow() Workflow { return j.owner.Workflow() }

// Steps yields the job's top-level steps.
func (j Job) Steps() iter.Seq[Step] { return boundSteps(j.owner, j.stepNodes()) }

// AllSteps yields every step of the job, each parallel group before the steps
// inside it, in the order of workflowlang.AllSteps.
func (j Job) AllSteps() iter.Seq[Step] {
	return func(yield func(Step) bool) {
		for step := range workflowlang.AllSteps(j.stepNodes()) {
			if !yield(Step{step, j.owner}) {
				return
			}
		}
	}
}

func (j Job) stepNodes() []*workflowlang.Step {
	if !j.Present() {
		return nil
	}
	return j.Job.Steps
}

func boundSteps(owner *Bound, nodes []*workflowlang.Step) iter.Seq[Step] {
	return func(yield func(Step) bool) {
		for _, step := range nodes {
			if step != nil && !yield(Step{step, owner}) {
				return
			}
		}
	}
}

// Step is a parsed step bound to its document.
type Step struct {
	*workflowlang.Step
	owner *Bound
}

func (s Step) Present() bool { return s.Step != nil }

func (s Step) Reference() source.Reference {
	if !s.Present() || s.owner == nil {
		return source.Reference{}
	}
	return s.owner.references.Steps[s.Step]
}

func (s Step) SourceReference() source.Reference {
	if !s.Present() || s.owner == nil {
		return source.Reference{}
	}
	return s.owner.Reference()
}

func (s Step) ContentText() string {
	if !s.Present() || s.owner == nil {
		return ""
	}
	return s.owner.text
}

func (s Step) DeclaringJob() string {
	if !s.Present() || s.owner == nil {
		return ""
	}
	return s.owner.references.StepJobs[s.Step]
}

// Job returns the job that declares this step, including nested parallel
// steps. Composite metadata without a declaring workflow job returns an absent Job.
func (s Step) Job() Job {
	if !s.Present() || s.owner == nil {
		return Job{}
	}
	job, _ := s.owner.Workflow().Job(s.DeclaringJob())
	return job
}

func (s Step) Workflow() Workflow { return s.owner.Workflow() }

// Condition decodes the step's if condition.
func (s Step) Condition() value.Value {
	if !s.Present() {
		return value.Value{}
	}
	return condition(s.If)
}

func (s Step) Run() (*workflowlang.ExecRun, bool) {
	if !s.Present() {
		return nil, false
	}
	run, ok := s.Exec.(*workflowlang.ExecRun)
	return run, ok && run != nil
}

func (s Step) Action() (*workflowlang.ExecAction, bool) {
	if !s.Present() {
		return nil, false
	}
	action, ok := s.Exec.(*workflowlang.ExecAction)
	return action, ok && action != nil
}

func (s Step) Wait() (*workflowlang.ExecWait, bool) {
	if !s.Present() {
		return nil, false
	}
	wait, ok := s.Exec.(*workflowlang.ExecWait)
	return wait, ok && wait != nil
}

func (s Step) Cancel() (*workflowlang.ExecCancel, bool) {
	if !s.Present() {
		return nil, false
	}
	cancel, ok := s.Exec.(*workflowlang.ExecCancel)
	return cancel, ok && cancel != nil
}

// ParallelSteps yields the steps of a parallel group; it reports false for other steps.
func (s Step) ParallelSteps() (iter.Seq[Step], bool) {
	if !s.Present() {
		return nil, false
	}
	parallel, ok := s.Exec.(*workflowlang.ExecParallel)
	if !ok || parallel == nil {
		return nil, false
	}
	return boundSteps(s.owner, parallel.Steps), true
}

// ActionBody exposes composite steps without a workflow lifecycle.
type ActionBody struct{ workflow Workflow }

func (a ActionBody) Present() bool      { return a.workflow.Present() }
func (a ActionBody) Workflow() Workflow { return a.workflow }
func (a ActionBody) Using() string {
	if a.workflow.owner == nil {
		return ""
	}
	return a.workflow.owner.using
}

func (a ActionBody) Steps() iter.Seq[Step] {
	job, _ := a.workflow.Job("")
	return job.Steps()
}

// OutputValues yields composite output expressions evaluated after all steps.
func (a ActionBody) OutputValues() iter.Seq[*workflowlang.String] {
	return func(yield func(*workflowlang.String) bool) {
		job, ok := a.workflow.Job("")
		if !ok {
			return
		}
		for _, output := range job.Outputs {
			if output != nil && !yield(output.Value) {
				return
			}
		}
	}
}
