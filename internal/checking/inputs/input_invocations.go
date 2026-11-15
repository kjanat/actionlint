package inputs

import (
	"errors"
	"fmt"
	"slices"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
)

var ErrInvalidCallGraph = errors.New("analyzer: invalid invocation graph")

func invalidGraph(format string, values ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidCallGraph, fmt.Sprintf(format, values...))
}

// ValidateOccurrence checks every supplied field against the registered execution.
func ValidateOccurrence(graph invocation.Graph, actual invocation.Script) error {
	if want, exists := graph.Script(actual.ID); !exists || want != actual {
		return invalidGraph("script %q disagrees with registered occurrence", actual.ID)
	}
	return nil
}

// ValidateInvocations checks supplied facts against the captured occurrence graph.
func (input Input) ValidateInvocations() error {
	available := slices.Contains(input.AvailableInputs, rulemeta.Invocations)
	if available != input.CallGraph.Valid() {
		return invalidGraph("invocations availability disagrees with graph presence")
	}
	textsAvailable := slices.Contains(input.AvailableInputs, rulemeta.Scripts)
	if len(input.Scripts)+len(input.Invoked)+len(input.ActionInputs) > 0 && !textsAvailable {
		return invalidGraph("script text supplied without the scripts input")
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
				if script.Occurrence != (invocation.Script{}) {
					return invalidGraph("script occurrence supplied without invocation graph")
				}
			}
		}
		for _, input := range input.ActionInputs {
			if input.Occurrence != (invocation.Script{}) {
				return invalidGraph("action input occurrence supplied without invocation graph")
			}
		}
		return nil
	}
	if input.Reference != input.CallGraph.Entry().Reference {
		return invalidGraph("input reference disagrees with graph entry")
	}
	selected := make(map[invocation.ID]bool, len(input.SelectedScripts))
	expected := input.CallGraph.ScriptCount()
	if input.SelectedScripts != nil {
		if !textsAvailable {
			return invalidGraph("script selection supplied without the scripts input")
		}
		expected = len(input.SelectedScripts)
		for _, id := range input.SelectedScripts {
			if _, exists := input.CallGraph.Script(id); !exists || selected[id] {
				return invalidGraph("unknown or duplicate selected script %q", id)
			}
			selected[id] = true
		}
	}
	seen := make(map[invocation.ID]bool)
	check := func(script Script, kind invocation.ScriptKind, entry bool) error {
		actual := script.Occurrence
		if err := ValidateOccurrence(input.CallGraph, actual); err != nil {
			return err
		}
		if (input.SelectedScripts != nil && !selected[actual.ID]) || seen[actual.ID] || script.Reference != actual.Declaration {
			return invalidGraph("script %q disagrees with registered occurrence", actual.ID)
		}
		if actual.Kind != kind || kind == invocation.ScriptRun && (actual.Owner == input.CallGraph.RootID()) != entry {
			return invalidGraph("script %q is in the wrong input collection", actual.ID)
		}
		seen[actual.ID] = true
		return nil
	}
	for _, script := range input.Scripts {
		if err := check(script, invocation.ScriptRun, true); err != nil {
			return err
		}
	}
	for _, script := range input.Invoked {
		if err := check(script, invocation.ScriptRun, false); err != nil {
			return err
		}
	}
	for _, input := range input.ActionInputs {
		if err := check(input.Script, invocation.ScriptActionInput, false); err != nil {
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
