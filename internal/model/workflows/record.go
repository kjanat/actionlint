// Package workflows binds parsed workflow and action syntax to its source document.
package workflows

import (
	"iter"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

// BoundDocument indexes steps and references at Bind. Changes to the tree after Bind leave those indexes stale.
type BoundDocument struct {
	text       string
	using      string
	workflow   *definition.Workflow
	references facts.References
	steps      map[sourcetext.Reference]*definition.Step
}

// Bind takes the action's runs.using value in using, empty for workflows.
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

func (b *BoundDocument) HasStep(step BoundStep) bool      { return step.Present() && b == step.document }
func (b *BoundDocument) HasWorkflow(w BoundWorkflow) bool { return w.Present() && b == w.document }
func (b *BoundDocument) HasJob(job BoundJob) bool         { return job.Present() && b == job.document }

// SourceIndex returns the zero index for a nil BoundDocument.
func (b *BoundDocument) SourceIndex() facts.SourceIndex {
	if b == nil {
		return facts.SourceIndex{}
	}
	return b.references.SourceIndex()
}

func (b *BoundDocument) ActionBody() ActionBody { return ActionBody{workflow: b.Workflow()} }

type BoundWorkflow struct {
	*definition.Workflow
	document *BoundDocument
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
	return BoundJob{job, w.document, key}, true
}

// Jobs keys jobs by lowercase ID.
func (w BoundWorkflow) Jobs() iter.Seq2[string, BoundJob] {
	return func(yield func(string, BoundJob) bool) {
		if !w.Present() {
			return
		}
		for key, job := range w.Workflow.Jobs {
			if job != nil && !yield(key, BoundJob{job, w.document, key}) {
				return
			}
		}
	}
}

// ScalarSource is available for indexed expressions, conditions and dependency references.
func (w BoundWorkflow) ScalarSource(s *definition.String) *definition.ScriptSource {
	pos, ok := s.Position()
	if !ok || !w.Present() {
		return nil
	}
	return w.ExpressionSources[pos]
}

type BoundJob struct {
	*definition.Job
	document *BoundDocument
	key      string
}

func (j BoundJob) Present() bool { return j.Job != nil }

func (j BoundJob) Key() string { return j.key }

func (j BoundJob) Reference() sourcetext.Reference {
	if !j.Present() || j.document == nil {
		return sourcetext.Reference{}
	}
	return j.document.references.Jobs[j.Key()]
}

func (j BoundJob) Workflow() BoundWorkflow { return j.document.Workflow() }

func (j BoundJob) Steps() iter.Seq[BoundStep] { return boundSteps(j.document, j.stepNodes()) }

// AllSteps yields each parallel group before the steps inside it.
func (j BoundJob) AllSteps() iter.Seq[BoundStep] {
	return func(yield func(BoundStep) bool) {
		for step := range definition.AllSteps(j.stepNodes()) {
			if !yield(BoundStep{step, j.document}) {
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

func boundSteps(document *BoundDocument, nodes []*definition.Step) iter.Seq[BoundStep] {
	return func(yield func(BoundStep) bool) {
		for _, step := range nodes {
			if step != nil && !yield(BoundStep{step, document}) {
				return
			}
		}
	}
}

type BoundStep struct {
	*definition.Step
	document *BoundDocument
}

func (s BoundStep) Present() bool { return s.Step != nil }

func (s BoundStep) Reference() sourcetext.Reference {
	if !s.Present() || s.document == nil {
		return sourcetext.Reference{}
	}
	return s.document.references.Steps[s.Step]
}

func (s BoundStep) SourceReference() sourcetext.Reference {
	if !s.Present() || s.document == nil {
		return sourcetext.Reference{}
	}
	return s.document.Reference()
}

func (s BoundStep) ContentText() string {
	if !s.Present() || s.document == nil {
		return ""
	}
	return s.document.text
}

func (s BoundStep) DeclaringJob() string {
	if !s.Present() || s.document == nil {
		return ""
	}
	return s.document.references.StepJobs[s.Step]
}

// Job returns an absent Job for composite metadata.
func (s BoundStep) Job() BoundJob {
	if !s.Present() || s.document == nil {
		return BoundJob{}
	}
	job, _ := s.document.Workflow().Job(s.DeclaringJob())
	return job
}

func (s BoundStep) Workflow() BoundWorkflow { return s.document.Workflow() }

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

func (s BoundStep) ParallelSteps() (iter.Seq[BoundStep], bool) {
	if !s.Present() {
		return nil, false
	}
	parallel, ok := s.Exec.(*definition.ExecParallel)
	if !ok || parallel == nil {
		return nil, false
	}
	return boundSteps(s.document, parallel.Steps), true
}

type ActionBody struct{ workflow BoundWorkflow }

func (a ActionBody) Present() bool           { return a.workflow.Present() }
func (a ActionBody) Workflow() BoundWorkflow { return a.workflow }
func (a ActionBody) Using() string {
	if a.workflow.document == nil {
		return ""
	}
	return a.workflow.document.using
}

func (a ActionBody) Steps() iter.Seq[BoundStep] {
	job, _ := a.workflow.Job("")
	return job.Steps()
}

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
