package workflows

import (
	"iter"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// IgnoreOrigin distinguishes executions of shared YAML anchors.
type IgnoreOrigin struct {
	StepPosition                *sourcetext.Position
	Job, StepID, StepName, Uses string
	Metadata                    bool
}

func (origin *IgnoreOrigin) Matches(job, step, uses string) bool {
	return (job == "" || job == origin.Job) && (step == "" || step == origin.StepID || step == origin.StepName) && (uses == "" || uses == origin.Uses)
}

func originSelector(s *definition.String) string {
	if s == nil {
		return ""
	}
	if literal := syntax.LiteralExpressionValue(s.Value); literal != nil {
		return *literal
	}
	return s.Value
}

// IgnoreOrigin returns a new value owned by the caller on each call.
func (j BoundJob) IgnoreOrigin() *IgnoreOrigin {
	if !j.Present() {
		return &IgnoreOrigin{}
	}
	origin := &IgnoreOrigin{Job: j.ID.Text()}
	if j.WorkflowCall != nil {
		origin.Uses = originSelector(j.WorkflowCall.Uses)
	}
	return origin
}

// IgnoreOrigin returns a new value owned by the caller on each call.
func (s BoundStep) IgnoreOrigin(job string) *IgnoreOrigin {
	if !s.Present() {
		return &IgnoreOrigin{Job: job}
	}
	origin := &IgnoreOrigin{Job: job, StepID: originSelector(s.ID), StepName: originSelector(s.Name)}
	if position, ok := s.Position(); ok {
		origin.StepPosition = &position
	}
	if action, ok := s.Action(); ok {
		origin.Uses = originSelector(action.Uses)
	}
	return origin
}

func (s BoundStep) Position() (sourcetext.Position, bool) {
	if !s.Present() || s.Pos == nil {
		return sourcetext.Position{}, false
	}
	return *s.Pos, true
}

type inputSource struct {
	input  *definition.ActionArgument
	unique bool
}

// InputNames yields the resolved with: keys of an action step and the input
// declaring each key, or nil when insertions make the declaring input ambiguous.
// It reports whether the key set is complete.
func InputNames(action *definition.ExecAction) (iter.Seq2[string, *definition.ActionArgument], bool) {
	resolved := ResolvedInputs(action)
	var sources map[sourcetext.Position]inputSource
	if action != nil {
		sources = actionInputSourceIndex(action)
	}
	return func(yield func(string, *definition.ActionArgument) bool) {
		for key := range resolved.KeyedEntries() {
			name, known := key.ScalarString()
			if !known {
				continue
			}
			position := key.Origin().Span.Start
			source, ok := sources[position]
			var input *definition.ActionArgument
			if ok && source.unique {
				input = source.input
			}
			if !yield(name, input) {
				return
			}
		}
	}, resolved.Complete()
}

func actionInputSourceIndex(action *definition.ExecAction) map[sourcetext.Position]inputSource {
	index := map[sourcetext.Position]inputSource{}
	add := func(input *definition.ActionArgument) {
		if input == nil || input.Name == nil || input.Name.Pos == nil {
			return
		}
		position := *input.Name.Pos
		entry := index[position]
		if entry.input == nil {
			entry.input, entry.unique = input, true
		} else {
			entry.unique = false
		}
		index[position] = entry
	}
	var collect func(*definition.ActionInputMapping)
	collect = func(mapping *definition.ActionInputMapping) {
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

func (j BoundJob) Position() (sourcetext.Position, bool) {
	if !j.Present() || j.Pos == nil {
		return sourcetext.Position{}, false
	}
	return *j.Pos, true
}

func (w BoundWorkflow) Position() (sourcetext.Position, bool) {
	if !w.Present() || w.Pos == nil {
		return sourcetext.Position{}, false
	}
	return *w.Pos, true
}

func (s BoundStep) Kind() (definition.ExecKind, bool) {
	if !s.Present() || s.Exec == nil {
		return 0, false
	}
	return s.Exec.Kind(), true
}

func (a ActionBody) ExpressionSources() map[sourcetext.Position]*definition.ScriptSource {
	if !a.Present() {
		return nil
	}
	return a.workflow.ExpressionSources
}
