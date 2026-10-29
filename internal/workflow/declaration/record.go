// Package declaration owns published syntax storage. Mutable decoded values enter
// only through Publish; consumers receive private read capabilities.
package declaration

import (
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/mapping"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	"actionlint.kjanat.dev/v2/internal/workflow/matrix"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
	"actionlint.kjanat.dev/v2/source"
)

type inputSource struct {
	input  *workflowlang.Input
	unique bool
}

type builder struct {
	steps            []*workflowlang.Step
	jobs             []*workflowlang.Job
	stringNodes      []*workflowlang.String
	boolNodes        []*workflowlang.Bool
	runnerNodes      []*workflowlang.Runner
	defaultsRunNodes []*workflowlang.DefaultsRun
	containerNodes   []*workflowlang.Container
	envNodes         []*workflowlang.Env
	execActionNodes  []*workflowlang.ExecAction
	strategyNodes    []*workflowlang.Strategy
}

// Record owns one private copy of the decoded model and its published facts.
// It retains no pointers into the caller's mutable input.
type Record struct {
	text         string
	action       bool
	using        string
	workflow     *workflowlang.Workflow
	skeleton     workflowfacts.Skeleton
	steps        []*workflowlang.Step
	jobs         []*workflowlang.Job
	stepIndices  map[*workflowlang.Step]int
	jobIndices   map[*workflowlang.Job]int
	jobKeys      map[*workflowlang.Job]string
	inputSources map[*workflowlang.ExecAction]map[workflowlang.Pos]inputSource
	strings      map[*workflowlang.String]value.Value
	bools        map[*workflowlang.Bool]value.Value
	conditions   map[*workflowlang.String]value.Value
	selections   map[*workflowlang.Runner]value.Value
	defaultsRun  map[*workflowlang.DefaultsRun]value.Value
	containers   map[*workflowlang.Container]value.Value
	environments map[*workflowlang.Env]mapping.Mapping
	resolved     map[*workflowlang.ExecAction]mapping.Mapping
	matrices     map[*workflowlang.Strategy]matrix.Resolution
	matrixMaps   map[*workflowlang.Matrix]mapping.Mapping
}

func Publish(text string, workflow *workflowlang.Workflow, action bool, using string) *Record {
	b := builder{}
	owned := b.cloneWorkflow(workflow)
	record := &Record{
		text: text, action: action, using: using, workflow: owned,
		skeleton: workflowfacts.BuildSkeleton(owned, action),
		steps:    b.steps, jobs: b.jobs,
		stepIndices: map[*workflowlang.Step]int{}, jobIndices: map[*workflowlang.Job]int{},
		jobKeys: map[*workflowlang.Job]string{}, inputSources: map[*workflowlang.ExecAction]map[workflowlang.Pos]inputSource{},
		strings: map[*workflowlang.String]value.Value{}, bools: map[*workflowlang.Bool]value.Value{},
		conditions: map[*workflowlang.String]value.Value{}, selections: map[*workflowlang.Runner]value.Value{},
		defaultsRun: map[*workflowlang.DefaultsRun]value.Value{}, containers: map[*workflowlang.Container]value.Value{},
		environments: map[*workflowlang.Env]mapping.Mapping{}, resolved: map[*workflowlang.ExecAction]mapping.Mapping{},
		matrices: map[*workflowlang.Strategy]matrix.Resolution{}, matrixMaps: map[*workflowlang.Matrix]mapping.Mapping{},
	}
	record.publishSemantics(&b)
	for i, step := range record.steps {
		record.stepIndices[step] = i
		if action, ok := step.Exec.(*workflowlang.ExecAction); ok && action != nil {
			record.inputSources[action] = actionInputSourceIndex(action)
		}
	}
	for i, job := range record.jobs {
		record.jobIndices[job] = i
	}
	if owned != nil {
		for key, job := range owned.Jobs {
			record.jobKeys[job] = key
		}
	}
	return record
}

func actionInputSourceIndex(action *workflowlang.ExecAction) map[workflowlang.Pos]inputSource {
	index := map[workflowlang.Pos]inputSource{}
	add := func(input *workflowlang.Input) {
		if input == nil || input.Name == nil || input.Name.Pos == nil {
			return
		}
		position := *input.Name.Pos
		entry := index[position]
		if entry.input == nil {
			entry.input = input
			entry.unique = true
		} else {
			entry.unique = false
		}
		index[position] = entry
	}
	var collect func(*workflowlang.ActionInputMapping)
	collect = func(mapping *workflowlang.ActionInputMapping) {
		if mapping == nil {
			return
		}
		for _, input := range mapping.Inputs {
			add(input)
		}
		for _, insertion := range mapping.Insertions {
			collect(insertion)
		}
	}
	for _, input := range action.Inputs {
		add(input)
	}
	for _, insertion := range action.InputInsertions {
		collect(insertion)
	}
	return index
}

func (r *Record) Text() string { return r.text }
func (r *Record) Action() bool { return r.action }

// Bound associates a parse record with one registry's document identity.
type Bound struct {
	record     *Record
	references workflowfacts.References
	steps      map[source.Reference]int
}

func (r *Record) Bind(identity string) *Bound {
	refs := r.skeleton.Bind(identity, []byte(r.text))
	bound := &Bound{record: r, references: refs, steps: map[source.Reference]int{}}
	for step, ref := range refs.Steps {
		bound.steps[ref] = r.stepIndices[step]
	}
	return bound
}

func (b *Bound) Reference() source.Reference { return b.references.Document }
func (b *Bound) Workflow() WorkflowView      { return makeWorkflowView(b, b.record.workflow) }

func (b *Bound) Step(reference source.Reference) (StepView, bool) {
	index, ok := b.steps[reference]
	if !ok {
		return StepView{}, false
	}
	return StepView{owner: b, index: index}, true
}

func (b *Bound) Owns(step StepView) bool {
	return step.Present() && b == step.owner
}

func (b *Bound) Declarations() ([]analyzer.Mapping, []analyzer.Dependency) {
	return b.declarations()
}

func (b *Bound) OwnsWorkflow(view WorkflowView) bool { return view.Present() && b == view.owner }
func (b *Bound) OwnsJob(view JobView) bool           { return view.Present() && b == view.owner }

func (b *Bound) SourceIndex() workflowfacts.SourceIndex { return b.references.SourceIndex() }
