package declaration

import (
	"iter"

	"actionlint.kjanat.dev/v2/internal/input/source"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// Origin is a detached, pointer-free suppression selector projection.
type Origin struct {
	Position                    source.Position
	HasPosition                 bool
	Job, StepID, StepName, Uses string
}

func originSelector(s *workflowlang.String) string {
	if s == nil {
		return ""
	}
	if literal := exprsyntax.LiteralExpressionValue(s.Value); literal != nil {
		return *literal
	}
	return s.Value
}

func (j Job) Origin() Origin {
	if !j.Present() {
		return Origin{}
	}
	origin := Origin{Job: j.ID.Text()}
	if j.WorkflowCall != nil {
		origin.Uses = originSelector(j.WorkflowCall.Uses)
	}
	return origin
}

func (s Step) Origin(job string) Origin {
	if !s.Present() {
		return Origin{Job: job}
	}
	origin := Origin{Job: job, StepID: originSelector(s.ID), StepName: originSelector(s.Name)}
	origin.Position, origin.HasPosition = s.Position()
	if action, ok := s.Action(); ok {
		origin.Uses = originSelector(action.Uses)
	}
	return origin
}

// Position returns the step's source position.
func (s Step) Position() (source.Position, bool) {
	if !s.Present() || s.Pos == nil {
		return source.Position{}, false
	}
	return *s.Pos, true
}

type inputSource struct {
	input  *workflowlang.Input
	unique bool
}

// InputNames yields the resolved with: keys of an action step and the input
// declaring each key, or nil when insertions make the declaring input ambiguous.
// It reports whether the key set is complete.
func InputNames(action *workflowlang.ExecAction) (iter.Seq2[string, *workflowlang.Input], bool) {
	resolved := ResolvedInputs(action)
	var sources map[source.Position]inputSource
	if action != nil {
		sources = actionInputSourceIndex(action)
	}
	return func(yield func(string, *workflowlang.Input) bool) {
		for key := range resolved.KeyedEntries() {
			name, known := key.ScalarString()
			if !known {
				continue
			}
			position := key.Origin().Span.Start
			source, ok := sources[source.Position{Line: position.Line, Column: position.Column}]
			var input *workflowlang.Input
			if ok && source.unique {
				input = source.input
			}
			if !yield(name, input) {
				return
			}
		}
	}, resolved.Complete()
}

func actionInputSourceIndex(action *workflowlang.ExecAction) map[source.Position]inputSource {
	index := map[source.Position]inputSource{}
	add := func(input *workflowlang.Input) {
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

// Position returns the job's source position.
func (j Job) Position() (source.Position, bool) {
	if !j.Present() || j.Pos == nil {
		return source.Position{}, false
	}
	return *j.Pos, true
}

// Position returns the position of the workflow's first mapping key.
func (w Workflow) Position() (source.Position, bool) {
	if !w.Present() || w.Pos == nil {
		return source.Position{}, false
	}
	return *w.Pos, true
}

// Kind reports the step's execution kind; it reports false when the step has no execution.
func (s Step) Kind() (workflowlang.ExecKind, bool) {
	if !s.Present() || s.Exec == nil {
		return 0, false
	}
	return s.Exec.Kind(), true
}

// ExpressionSources maps decoded action scalars to their captured YAML.
func (a ActionBody) ExpressionSources() map[source.Position]*workflowlang.ScriptSource {
	if !a.Present() {
		return nil
	}
	return a.workflow.ExpressionSources
}
