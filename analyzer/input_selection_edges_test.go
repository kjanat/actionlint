package analyzer

import (
	"errors"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/source"
)

func TestInputValidatesActionInputAndScriptCollections(t *testing.T) {
	graph := graphFixture(t)
	entryID, callID := graph.Entry.ID, graph.Calls[0].ID
	rootRun := ScriptOccurrence{
		ID: OccurrenceID(strings.Repeat("d", 64)), Owner: entryID, Ancestry: []OccurrenceID{entryID},
		Depth: 0, Declaration: source.Reference{Document: "workflow", Revision: "r", Node: "root-run"}, Kind: RunScript,
	}
	invokedRun := graph.Scripts[0]
	actionInput := ScriptOccurrence{
		ID: OccurrenceID(strings.Repeat("e", 64)), Owner: callID, Ancestry: []OccurrenceID{entryID, callID},
		Depth: 1, Declaration: source.Reference{Document: "action", Revision: "r", Node: "input"}, Kind: ActionInputScript, Input: "script",
	}
	graph.Scripts = []ScriptOccurrence{rootRun, invokedRun, actionInput}
	graph.Budget.MaxScripts = len(graph.Scripts)
	rootText := Script{Reference: rootRun.Declaration, Occurrence: rootRun, Text: "root"}
	invokedText := Script{Reference: invokedRun.Declaration, Occurrence: invokedRun, Text: "invoked"}
	inputText := Script{Reference: actionInput.Declaration, Occurrence: actionInput, Text: "input"}
	valid := func() Input {
		return Input{
			Reference: graph.Entry.Reference, Available: []Fact{Invocations, Scripts}, CallGraph: retainGraph(t, graph),
			Scripts: []Script{rootText}, Invoked: []Script{invokedText},
			ActionInputs: []ActionInput{{Script: inputText, Site: ScriptInput{Action: "owner/action", Input: "script", Language: "javascript"}}},
		}
	}
	if err := valid().ValidateInvocations(); err != nil {
		t.Fatalf("valid action input rejected: %v", err)
	}
	for name, change := range map[string]func(*Input){
		"action input shell":    func(input *Input) { input.ActionInputs[0].Shell = "bash" },
		"action input runtime":  func(input *Input) { input.ActionInputs[0].Runtime = &Runtime{} },
		"action input action":   func(input *Input) { input.ActionInputs[0].Site.Action = "invalid" },
		"action input language": func(input *Input) { input.ActionInputs[0].Site.Language = "" },
		"entry run in invoked collection": func(input *Input) {
			input.Scripts = nil
			input.Invoked = []Script{rootText, invokedText}
		},
		"invoked run in scripts collection": func(input *Input) {
			input.Invoked = nil
			input.Scripts = []Script{rootText, invokedText}
		},
		"action input in scripts collection": func(input *Input) {
			input.ActionInputs = nil
			input.Scripts = []Script{rootText, inputText}
		},
		"action input in invoked collection": func(input *Input) {
			input.ActionInputs = nil
			input.Invoked = []Script{invokedText, inputText}
		},
		"action input selector mismatch": func(input *Input) {
			input.ActionInputs[0].Site.Input = "other"
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := valid()
			change(&input)
			if err := input.ValidateInvocations(); !errors.Is(err, ErrInvalidCallGraph) {
				t.Fatalf("invalid collection accepted: %v", err)
			}
		})
	}
}

func TestCollectedInvocationGraphCanHaveZeroScripts(t *testing.T) {
	graph := graphFixture(t)
	graph.Scripts = nil
	input := Input{
		Reference: graph.Entry.Reference, Available: []Fact{Invocations, Scripts}, CallGraph: retainGraph(t, graph),
		SelectedScripts: []OccurrenceID{},
	}
	if err := input.ValidateInvocations(); err != nil {
		t.Fatalf("valid empty script collection rejected: %v", err)
	}
}
