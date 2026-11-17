// Package workflows binds parsed workflow and action syntax to its source
// document. Consumers read the parsed tree directly; BoundWorkflow, BoundJob and BoundStep
// handles add the owning document's identity and semantic lookups.
package workflows

import (
	"iter"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

// BoundDocument is one decoded document bound to a registry's document identity. Over
// the parsed tree it adds the source references derived from that identity and
// the source text, so views can name and locate their declarations. Bind takes
// ownership of the tree: callers must not modify it afterwards.
type BoundDocument struct {
	text       string
	using      string
	workflow   *definition.Workflow
	references facts.References
	steps      map[sourcetext.Reference]*definition.Step
}

// Bind indexes workflow under identity. action selects action-manifest step
// addresses; using is the action's runs.using value, empty for workflows.
func Bind(identity, text string, workflow *definition.Workflow, action bool, using string) *BoundDocument {
	refs := facts.Index(identity, []byte(text), workflow, action)
	bound := &BoundDocument{text: text, using: using, workflow: workflow, references: refs, steps: map[sourcetext.Reference]*definition.Step{}}
	for step, ref := range refs.Steps {
		bound.steps[ref] = step
	}
	return bound
}

func (b *BoundDocument) Reference() sourcetext.Reference { return b.references.Document }
func (b *BoundDocument) Workflow() BoundWorkflow {
	if b == nil || b.workflow == nil {
		return BoundWorkflow{}
	}
	return BoundWorkflow{b.workflow, b}
}

func (b *BoundDocument) Step(reference sourcetext.Reference) (BoundStep, bool) {
	step, ok := b.steps[reference]
	if !ok {
		return BoundStep{}, false
	}
	return BoundStep{step, b}, true
}

func (b *BoundDocument) Owns(step BoundStep) bool          { return step.Present() && b == step.owner }
func (b *BoundDocument) OwnsWorkflow(w BoundWorkflow) bool { return w.Present() && b == w.owner }
func (b *BoundDocument) OwnsJob(job BoundJob) bool         { return job.Present() && b == job.owner }

// SourceIndex returns detached source identities and positions. A nil BoundDocument
// returns the zero index.
func (b *BoundDocument) SourceIndex() facts.SourceIndex {
	if b == nil {
		return facts.SourceIndex{}
	}
	return b.references.SourceIndex()
}

// ActionBody exposes composite action steps without a workflow lifecycle.
func (b *BoundDocument) ActionBody() ActionBody { return ActionBody{workflow: b.Workflow()} }

// BoundWorkflow is a parsed workflow bound to its document.
type BoundWorkflow struct {
	*definition.Workflow
	owner *BoundDocument
}

func (w BoundWorkflow) Present() bool { return w.Workflow != nil }

func (w BoundWorkflow) Job(key string) (BoundJob, bool) {
	if !w.Present() {
		return BoundJob{}, false
	}
	job, ok := w.Workflow.Jobs[key]
	if !ok || job == nil {
		return BoundJob{}, false
	}
	return BoundJob{job, w.owner, key}, true
}

// Jobs yields the workflow's jobs keyed by lowercase ID.
func (w BoundWorkflow) Jobs() iter.Seq2[string, BoundJob] {
	return func(yield func(string, BoundJob) bool) {
		if !w.Present() {
			return
		}
		for key, job := range w.Workflow.Jobs {
			if job != nil && !yield(key, BoundJob{job, w.owner, key}) {
				return
			}
		}
	}
}

// ScalarSource maps a decoded scalar back to its captured YAML. It is
// available for indexed expressions, conditions and dependency references.
func (w BoundWorkflow) ScalarSource(s *definition.String) *definition.ScriptSource {
	pos, ok := s.Position()
	if !ok || !w.Present() {
		return nil
	}
	return w.ExpressionSources[pos]
}

// BoundJob is a parsed job bound to its document.
type BoundJob struct {
	*definition.Job
	owner *BoundDocument
	key   string
}

func (j BoundJob) Present() bool { return j.Job != nil }

// Key returns the lowercase job ID that indexes the job in the workflow.
func (j BoundJob) Key() string { return j.key }

func (j BoundJob) Reference() sourcetext.Reference {
	if !j.Present() || j.owner == nil {
		return sourcetext.Reference{}
	}
	return j.owner.references.Jobs[j.Key()]
}

func (j BoundJob) Workflow() BoundWorkflow { return j.owner.Workflow() }

// Steps yields the job's top-level steps.
func (j BoundJob) Steps() iter.Seq[BoundStep] { return boundSteps(j.owner, j.stepNodes()) }

// AllSteps yields every step of the job, each parallel group before the steps
// inside it, in the order of definition.AllSteps.
func (j BoundJob) AllSteps() iter.Seq[BoundStep] {
	return func(yield func(BoundStep) bool) {
		for step := range definition.AllSteps(j.stepNodes()) {
			if !yield(BoundStep{step, j.owner}) {
				return
			}
		}
	}
}

func (j BoundJob) stepNodes() []*definition.Step {
	if !j.Present() {
		return nil
	}
	return j.Job.Steps
}

func boundSteps(owner *BoundDocument, nodes []*definition.Step) iter.Seq[BoundStep] {
	return func(yield func(BoundStep) bool) {
		for _, step := range nodes {
			if step != nil && !yield(BoundStep{step, owner}) {
				return
			}
		}
	}
}

// BoundStep is a parsed step bound to its document.
type BoundStep struct {
	*definition.Step
	owner *BoundDocument
}

func (s BoundStep) Present() bool { return s.Step != nil }

func (s BoundStep) Reference() sourcetext.Reference {
	if !s.Present() || s.owner == nil {
		return sourcetext.Reference{}
	}
	return s.owner.references.Steps[s.Step]
}

func (s BoundStep) SourceReference() sourcetext.Reference {
	if !s.Present() || s.owner == nil {
		return sourcetext.Reference{}
	}
	return s.owner.Reference()
}

func (s BoundStep) ContentText() string {
	if !s.Present() || s.owner == nil {
		return ""
	}
	return s.owner.text
}

func (s BoundStep) DeclaringJob() string {
	if !s.Present() || s.owner == nil {
		return ""
	}
	return s.owner.references.StepJobs[s.Step]
}

// Job returns the job that declares this step, including nested parallel
// steps. Composite metadata without a declaring workflow job returns an absent Job.
func (s BoundStep) Job() BoundJob {
	if !s.Present() || s.owner == nil {
		return BoundJob{}
	}
	job, _ := s.owner.Workflow().Job(s.DeclaringJob())
	return job
}

func (s BoundStep) Workflow() BoundWorkflow { return s.owner.Workflow() }

// Condition decodes the step's if condition.
func (s BoundStep) Condition() value.Value {
	if !s.Present() {
		return value.Value{}
	}
	return condition(s.If)
}

func (s BoundStep) Run() (*definition.ExecRun, bool) {
	if !s.Present() {
		return nil, false
	}
	run, ok := s.Exec.(*definition.ExecRun)
	return run, ok && run != nil
}

func (s BoundStep) Action() (*definition.ExecAction, bool) {
	if !s.Present() {
		return nil, false
	}
	action, ok := s.Exec.(*definition.ExecAction)
	return action, ok && action != nil
}

func (s BoundStep) Wait() (*definition.ExecWait, bool) {
	if !s.Present() {
		return nil, false
	}
	wait, ok := s.Exec.(*definition.ExecWait)
	return wait, ok && wait != nil
}

func (s BoundStep) Cancel() (*definition.ExecCancel, bool) {
	if !s.Present() {
		return nil, false
	}
	cancel, ok := s.Exec.(*definition.ExecCancel)
	return cancel, ok && cancel != nil
}

// ParallelSteps yields the steps of a parallel group; it reports false for other steps.
func (s BoundStep) ParallelSteps() (iter.Seq[BoundStep], bool) {
	if !s.Present() {
		return nil, false
	}
	parallel, ok := s.Exec.(*definition.ExecParallel)
	if !ok || parallel == nil {
		return nil, false
	}
	return boundSteps(s.owner, parallel.Steps), true
}

// ActionBody exposes composite steps without a workflow lifecycle.
type ActionBody struct{ workflow BoundWorkflow }

func (a ActionBody) Present() bool           { return a.workflow.Present() }
func (a ActionBody) Workflow() BoundWorkflow { return a.workflow }
func (a ActionBody) Using() string {
	if a.workflow.owner == nil {
		return ""
	}
	return a.workflow.owner.using
}

func (a ActionBody) Steps() iter.Seq[BoundStep] {
	job, _ := a.workflow.Job("")
	return job.Steps()
}

// OutputValues yields composite output expressions evaluated after all steps.
func (a ActionBody) OutputValues() iter.Seq[*definition.String] {
	return func(yield func(*definition.String) bool) {
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
