package analyzer

import (
	"slices"

	"actionlint.kjanat.dev/v2/source"
)

// ValidateInvocations checks supplied facts against the captured occurrence graph.
func (input Input) ValidateInvocations() error {
	available := slices.Contains(input.Available, Invocations)
	if available != input.CallGraph.Valid() {
		return invalidGraph("invocations availability disagrees with graph presence")
	}
	textsAvailable := slices.Contains(input.Available, Scripts)
	if len(input.Scripts)+len(input.Invoked)+len(input.ActionInputs) > 0 && !textsAvailable {
		return invalidGraph("script text supplied without scripts fact")
	}
	for _, actionInput := range input.ActionInputs {
		if actionInput.Shell != "" || actionInput.Runtime != nil {
			return invalidGraph("action input contains shell execution facts")
		}
		if err := ValidateScriptInput(actionInput.Site); err != nil {
			return invalidGraph("invalid action input selector: %v", err)
		}
	}
	if !input.CallGraph.Valid() {
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
	if input.Reference != input.CallGraph.Entry().Reference {
		return invalidGraph("input reference disagrees with graph entry")
	}
	selected := make(map[OccurrenceID]bool, len(input.SelectedScripts))
	expected := input.CallGraph.ScriptCount()
	if input.SelectedScripts != nil {
		if !textsAvailable {
			return invalidGraph("script selection supplied without scripts fact")
		}
		expected = len(input.SelectedScripts)
		for _, id := range input.SelectedScripts {
			if _, exists := input.CallGraph.script(id); !exists || selected[id] {
				return invalidGraph("unknown or duplicate selected script %q", id)
			}
			selected[id] = true
		}
	}
	seen := make(map[OccurrenceID]bool)
	check := func(script Script, kind ScriptKind, entry bool) error {
		actual := script.Occurrence
		if err := input.CallGraph.ValidateOccurrence(actual); err != nil {
			return err
		}
		if (input.SelectedScripts != nil && !selected[actual.ID]) || seen[actual.ID] || script.Reference != actual.Declaration {
			return invalidGraph("script %q disagrees with registered occurrence", actual.ID)
		}
		if actual.Kind != kind || kind == RunScript && (actual.Owner == input.CallGraph.Entry().ID) != entry {
			return invalidGraph("script %q is in the wrong input collection", actual.ID)
		}
		if legacy := script.Invocation; legacy != nil {
			call, found := input.CallGraph.Call(actual.Owner)
			caller := ""
			if call.Site.Document != input.CallGraph.Entry().Reference.Document {
				caller = call.Site.Document
			}
			if !found || legacy.Reference != call.Site || legacy.Depth != call.Depth || legacy.Step != call.Step || legacy.Uses != call.Uses || legacy.Caller != caller {
				return invalidGraph("script %q has conflicting legacy invocation", actual.ID)
			}
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
	if textsAvailable && len(seen) != expected {
		return invalidGraph("script text does not cover selected occurrences")
	}
	return nil
}

func emptyOccurrence(value ScriptOccurrence) bool {
	return value.ID == "" && value.Owner == "" && len(value.Ancestry) == 0 && value.Depth == 0 && value.Declaration == (source.Reference{}) && value.Kind == "" && value.Input == ""
}
