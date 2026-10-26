package analyzer

import (
	"slices"

	"actionlint.kjanat.dev/v2/source"
)

// ValidateInvocations checks supplied facts against the retained occurrence graph.
func (input Input) ValidateInvocations() error {
	available := slices.Contains(input.Available, Invocations)
	if available != (input.CallGraph != nil) {
		return invalidGraph("invocations availability disagrees with graph presence")
	}
	textsAvailable := slices.Contains(input.Available, Scripts)
	if len(input.Scripts)+len(input.Invoked)+len(input.ActionInputs) > 0 && !textsAvailable {
		return invalidGraph("script text supplied without scripts fact")
	}
	if input.CallGraph == nil {
		if input.SelectedScripts != nil {
			return invalidGraph("script selection supplied without invocation graph")
		}
		for _, scripts := range [][]Script{input.Scripts, input.Invoked} {
			for _, script := range scripts {
				if !emptyOccurrence(script.Occurrence) {
					return invalidGraph("script occurrence supplied without invocation graph")
				}
			}
		}
		for _, input := range input.ActionInputs {
			if !emptyOccurrence(input.Occurrence) {
				return invalidGraph("action input occurrence supplied without invocation graph")
			}
		}
		return nil
	}
	if err := input.CallGraph.Validate(); err != nil {
		return err
	}
	if input.Reference != input.CallGraph.Entry.Reference {
		return invalidGraph("input reference disagrees with graph entry")
	}
	registered := make(map[OccurrenceID]ScriptOccurrence, len(input.CallGraph.Scripts))
	for _, occurrence := range input.CallGraph.Scripts {
		registered[occurrence.ID] = occurrence
	}
	selected := make(map[OccurrenceID]bool, len(registered))
	if input.SelectedScripts == nil {
		for id := range registered {
			selected[id] = true
		}
	} else {
		if !textsAvailable {
			return invalidGraph("script selection supplied without scripts fact")
		}
		for _, id := range input.SelectedScripts {
			if _, exists := registered[id]; !exists || selected[id] {
				return invalidGraph("unknown or duplicate selected script %q", id)
			}
			selected[id] = true
		}
	}
	seen := make(map[OccurrenceID]bool)
	check := func(script Script, kind ScriptKind, entry bool) error {
		actual := script.Occurrence
		want, exists := registered[actual.ID]
		if !exists || !selected[actual.ID] || seen[actual.ID] || actual.Owner != want.Owner || actual.Depth != want.Depth || actual.Declaration != want.Declaration || actual.Kind != want.Kind || actual.Input != want.Input || !slices.Equal(actual.Ancestry, want.Ancestry) || script.Reference != want.Declaration {
			return invalidGraph("script %q disagrees with registered occurrence", actual.ID)
		}
		if actual.Kind != kind || kind == RunScript && (actual.Owner == input.CallGraph.Entry.ID) != entry {
			return invalidGraph("script %q is in the wrong input collection", actual.ID)
		}
		seen[actual.ID] = true
		return nil
	}
	for _, script := range input.Scripts {
		if err := check(script, RunScript, true); err != nil {
			return err
		}
	}
	for _, script := range input.Invoked {
		if err := check(script, RunScript, false); err != nil {
			return err
		}
	}
	for _, input := range input.ActionInputs {
		if err := check(input.Script, ActionInputScript, false); err != nil {
			return err
		}
		if input.Site.Input != input.Occurrence.Input {
			return invalidGraph("action input selector disagrees with occurrence")
		}
	}
	if textsAvailable && len(seen) != len(selected) {
		return invalidGraph("script text does not cover selected occurrences")
	}
	return nil
}

func emptyOccurrence(value ScriptOccurrence) bool {
	return value.ID == "" && value.Owner == "" && len(value.Ancestry) == 0 && value.Depth == 0 && value.Declaration == (source.Reference{}) && value.Kind == "" && value.Input == ""
}
