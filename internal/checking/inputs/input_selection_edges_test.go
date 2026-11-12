package inputs

import (
	"errors"
	"strings"
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	"actionlint.kjanat.dev/v2/internal/input/source"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
)

func TestInputValidatesActionInputAndScriptCollections(t *testing.T) {
	t.Parallel()
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
	rootText := Script{Reference: rootRun.Declaration, Text: "root", Occurrence: rootRun}
	invokedText := Script{Reference: invokedRun.Declaration, Text: "invoked", Occurrence: invokedRun}
	inputText := Script{Reference: actionInput.Declaration, Text: "input", Occurrence: actionInput}
	valid := func() Input {
		return Input{
			Reference: graph.Entry.Reference, AvailableInputs: []rulemeta.InputKind{rulemeta.Invocations, rulemeta.Scripts}, CallGraph: retainGraph(t, graph),
			Scripts: []Script{rootText}, Invoked: []Script{invokedText},
			ActionInputs: []ActionInput{{Script: inputText, Site: ScriptInput{Action: "owner/action", Input: "script", Language: "javascript"}}},
		}
	}
	if err := valid().ValidateInvocations(); err != nil {
		t.Fatalf("valid action input rejected: %v", err)
	}
	for name, change := range map[string]func(*Input){
		"action input shell":    func(input *Input) { input.ActionInputs[0].Shell = "bash" },
		"action input runtime":  func(input *Input) { input.ActionInputs[0].Runtime = &scriptmodel.Execution{} },
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
			t.Parallel()
			input := valid()
			change(&input)
			if err := input.ValidateInvocations(); !errors.Is(err, ErrInvalidCallGraph) {
				t.Fatalf("invalid collection accepted: %v", err)
			}
		})
	}
}

func TestCollectedInvocationGraphCanHaveZeroScripts(t *testing.T) {
	t.Parallel()
	graph := graphFixture(t)
	graph.Scripts = nil
	input := Input{
		Reference: graph.Entry.Reference, AvailableInputs: []rulemeta.InputKind{rulemeta.Invocations, rulemeta.Scripts}, CallGraph: retainGraph(t, graph),
		SelectedScripts: []OccurrenceID{},
	}
	if err := input.ValidateInvocations(); err != nil {
		t.Fatalf("valid empty script collection rejected: %v", err)
	}
}
