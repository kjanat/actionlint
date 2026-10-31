package declaration

import (
	"iter"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/mapping"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/matrix"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
	"actionlint.kjanat.dev/v2/source"
)

func (v StringView) Semantic() value.Value {
	if !v.Present() || v.owner == nil {
		return value.Value{}
	}
	return v.owner.record.strings[v.data()]
}

func (v BoolView) Semantic() value.Value {
	if !v.Present() || v.owner == nil {
		return value.Value{}
	}
	return v.owner.record.bools[v.data()]
}

func (v StepView) Condition() value.Value {
	if !v.Present() || v.owner == nil {
		return value.Value{}
	}
	return v.owner.record.conditions[v.data().If]
}

func (v EnvView) Mapping() mapping.Mapping {
	if !v.Present() || v.owner == nil {
		return mapping.Mapping{}
	}
	return v.owner.record.environments[v.data()]
}

func (v ExecActionView) ResolvedInputs() mapping.Mapping {
	if !v.Present() || v.owner == nil {
		return mapping.Mapping{}
	}
	return v.owner.record.resolved[v.data()]
}

func (v RunnerView) Selection() value.Value {
	if !v.Present() || v.owner == nil {
		return value.Value{}
	}
	return v.owner.record.selections[v.data()]
}

func (v DefaultsRunView) Semantic() value.Value {
	if !v.Present() || v.owner == nil {
		return value.Value{}
	}
	return v.owner.record.defaultsRun[v.data()]
}

func (v ContainerView) Semantic() value.Value {
	if !v.Present() || v.owner == nil {
		return value.Value{}
	}
	return v.owner.record.containers[v.data()]
}

func (v StrategyView) MatrixResolution() matrix.Resolution {
	if !v.Present() || v.owner == nil {
		return matrix.Resolution{}
	}
	return v.owner.record.matrices[v.data()]
}

func (v WorkflowView) Job(key string) (JobView, bool) { return v.JobsAt(key) }

func (v StepView) Reference() source.Reference {
	if !v.Present() {
		return source.Reference{}
	}
	return v.owner.references.Steps[v.data()]
}

func (v StepView) SourceReference() source.Reference {
	if !v.Present() {
		return source.Reference{}
	}
	return v.owner.Reference()
}

func (v StepView) ContentText() string {
	if !v.Present() {
		return ""
	}
	return v.owner.record.text
}

func (v StepView) DeclaringJob() string {
	if !v.Present() {
		return ""
	}
	return v.owner.references.StepJobs[v.data()]
}

func (v StepView) Run() (ExecRunView, bool)           { return v.Exec().Run() }
func (v StepView) Action() (ExecActionView, bool)     { return v.Exec().Action() }
func (v StepView) Parallel() (ExecParallelView, bool) { return v.Exec().Parallel() }
func (v StepView) Wait() (ExecWaitView, bool)         { return v.Exec().Wait() }
func (v StepView) Cancel() (ExecCancelView, bool)     { return v.Exec().Cancel() }

// Kind dispatches over the closed set of step executions instead of calling
// through the interface, so reading the kind cannot reach unsummarized code.
func (v ExecView) Kind() (workflowlang.ExecKind, bool) {
	switch v.node.(type) {
	case *workflowlang.ExecRun:
		return workflowlang.ExecKindRun, true
	case *workflowlang.ExecAction:
		return workflowlang.ExecKindAction, true
	case *workflowlang.ExecWait:
		return workflowlang.ExecKindWait, true
	case *workflowlang.ExecCancel:
		return workflowlang.ExecKindCancel, true
	case *workflowlang.ExecParallel:
		return workflowlang.ExecKindParallel, true
	default:
		return 0, false
	}
}

func (v JobView) Reference() source.Reference {
	if !v.Present() {
		return source.Reference{}
	}
	return v.owner.references.Jobs[v.owner.record.jobKeys[v.data()]]
}

func (v ExecActionView) InputNames() (iter.Seq2[string, InputView], bool) {
	resolved := mapping.Mapping{}
	var sources map[workflowlang.Pos]inputSource
	if v.Present() {
		resolved = v.owner.record.resolved[v.data()]
		sources = v.owner.record.inputSources[v.data()]
	}
	return func(yield func(string, InputView) bool) {
		for key := range resolved.KeyedEntries() {
			name, known := key.ScalarString()
			if !known {
				continue
			}
			position := key.Origin().Span.Start
			source, ok := sources[workflowlang.Pos{Line: position.Line, Col: position.Column}]
			if !ok || !source.unique {
				if !yield(name, InputView{}) {
					return
				}
				continue
			}
			if !yield(name, makeInputView(v.owner, source.input)) {
				return
			}
		}
	}, resolved.Complete()
}

func (v StringView) ContainsExpression() bool {
	return v.Present() && exprlang.ContainsExpression(v.Value())
}

func (v StringView) IsExpressionAssigned() bool {
	return v.Present() && workflowlang.IsExprAssigned(v.Value())
}

func (v ScriptSourceView) Position(line, column int) (workflowlang.Pos, bool) {
	pos, ok := v.data().Position(line, column)
	if !ok {
		return workflowlang.Pos{}, false
	}
	return *pos, true
}

func (v ScriptSourceView) EndPos(line, column int) (workflowlang.Pos, bool) {
	pos, ok := v.data().EndPos(line, column)
	if !ok {
		return workflowlang.Pos{}, false
	}
	return *pos, true
}

func (v ScriptSourceView) LineSpans() iter.Seq[workflowlang.ScriptLineSpan] {
	return func(yield func(workflowlang.ScriptLineSpan) bool) {
		for _, span := range v.data().LineSpans() {
			if !yield(span) {
				return
			}
		}
	}
}

// Origin is a detached, pointer-free suppression selector projection.
type Origin struct {
	Position                    workflowlang.Pos
	HasPosition                 bool
	Job, StepID, StepName, Uses string
}

func originSelector(value StringView) string {
	if literal := exprlang.LiteralExpressionValue(value.Value()); literal != nil {
		return *literal
	}
	return value.Value()
}

func (v JobView) Origin() Origin {
	if !v.Present() {
		return Origin{}
	}
	origin := Origin{Job: v.ID().Value()}
	if call := v.WorkflowCall(); call.Present() {
		origin.Uses = originSelector(call.Uses())
	}
	return origin
}

func (v StepView) Origin(job string) Origin {
	if !v.Present() {
		return Origin{Job: job}
	}
	origin := Origin{Job: job, StepID: originSelector(v.ID()), StepName: originSelector(v.Name())}
	origin.Position, origin.HasPosition = v.Pos()
	if action, present := v.Action(); present {
		origin.Uses = originSelector(action.Uses())
	}
	return origin
}

// ActionBodyView exposes composite steps without a workflow lifecycle.
type ActionBodyView struct{ workflow WorkflowView }

func (b *Bound) ActionBody() ActionBodyView { return ActionBodyView{workflow: b.Workflow()} }
func (v ActionBodyView) Present() bool      { return v.workflow.Present() }
func (v ActionBodyView) Using() string {
	if v.workflow.owner == nil {
		return ""
	}
	return v.workflow.owner.record.using
}

func (v ActionBodyView) Steps() iter.Seq[StepView] {
	job, _ := v.workflow.Job("")
	return job.Steps()
}

// ScalarLine is one decoded line's byte interval. It has no storage aliases.
type ScalarLine struct{ Start, End int }

func (v ScriptSourceView) LinesLen() int {
	if !v.Present() {
		return 0
	}
	return len(v.data().Lines)
}

func (v ScriptSourceView) Line(index int) (ScalarLine, bool) {
	if !v.Present() || index < 0 || index >= len(v.data().Lines) {
		return ScalarLine{}, false
	}
	line := v.data().Lines[index]
	return ScalarLine{Start: line.Start, End: line.End}, true
}

func (v ScriptSourceView) Lines() iter.Seq[ScalarLine] {
	return func(yield func(ScalarLine) bool) {
		if !v.Present() {
			return
		}
		for _, line := range v.data().Lines {
			if !yield(ScalarLine{Start: line.Start, End: line.End}) {
				return
			}
		}
	}
}

// SameDeclaration compares declaration identity within one frozen document.
func (v CacheModeView) SameDeclaration(other CacheModeView) bool {
	return v.Present() && other.Present() && v.owner == other.owner && v.node == other.node
}

func (v JobView) Key() string {
	if !v.Present() {
		return ""
	}
	return v.owner.record.jobKeys[v.data()]
}

// EventName dispatches over the closed set of events instead of calling
// through the interface, so reading the name cannot reach unsummarized code.
func (v EventView) EventName() string {
	switch node := v.node.(type) {
	case *workflowlang.WebhookEvent:
		return node.EventName()
	case *workflowlang.ScheduledEvent:
		return node.EventName()
	case *workflowlang.WorkflowDispatchEvent:
		return node.EventName()
	case *workflowlang.RepositoryDispatchEvent:
		return node.EventName()
	case *workflowlang.WorkflowCallEvent:
		return node.EventName()
	case *workflowlang.ImageVersionEvent:
		return node.EventName()
	default:
		return ""
	}
}
