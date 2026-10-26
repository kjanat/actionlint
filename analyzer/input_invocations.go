package analyzer

import "slices"

// ValidateInvocations checks supplied facts against the retained occurrence graph.
func (input Input) ValidateInvocations() error {
	available := slices.Contains(input.Available, Invocations)
	if available != (input.CallGraph != nil) {
		return invalidGraph("invocations availability disagrees with graph presence")
	}
	if input.CallGraph == nil {
		return nil
	}
	if err := input.CallGraph.Validate(); err != nil {
		return err
	}
	if input.Reference != input.CallGraph.Entry.Reference {
		return invalidGraph("input reference disagrees with graph entry")
	}
	if len(input.Scripts)+len(input.Invoked)+len(input.ActionInputs) > 0 && !slices.Contains(input.Available, Scripts) {
		return invalidGraph("script text supplied without scripts fact")
	}
	registered := make(map[OccurrenceID]ScriptOccurrence, len(input.CallGraph.Scripts))
	for _, occurrence := range input.CallGraph.Scripts {
		registered[occurrence.ID] = occurrence
	}
	seen := make(map[OccurrenceID]bool)
	check := func(script Script, kind ScriptKind, entry bool) error {
		actual := script.Occurrence
		want, exists := registered[actual.ID]
		if !exists || seen[actual.ID] || actual.Owner != want.Owner || actual.Depth != want.Depth || actual.Declaration != want.Declaration || actual.Kind != want.Kind || actual.Input != want.Input || !slices.Equal(actual.Ancestry, want.Ancestry) || script.Reference != want.Declaration {
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
	return nil
}
