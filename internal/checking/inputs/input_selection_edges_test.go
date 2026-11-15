package inputs

import (
	"errors"
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/invocationfixture"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
)

func TestInputValidatesActionInputAndScriptCollections(t *testing.T) {
	t.Parallel()
	graph := invocationfixture.Graph(t,
		invocationfixture.Script{Node: "root-run", Entry: true}, invocationfixture.Script{Node: "script"},
		invocationfixture.Script{Node: "input", Input: "script"})
	scripts := graph.Scripts()
	rootRun, invokedRun, actionInput := scripts[0], scripts[1], scripts[2]
	rootText := Script{Reference: rootRun.Declaration, Text: "root", Occurrence: rootRun}
	invokedText := Script{Reference: invokedRun.Declaration, Text: "invoked", Occurrence: invokedRun}
	inputText := Script{Reference: actionInput.Declaration, Text: "input", Occurrence: actionInput}
	valid := func() Input {
		return Input{
			Reference: graph.Entry().Reference, AvailableInputs: []rulemeta.InputKind{rulemeta.Invocations, rulemeta.Scripts}, CallGraph: graph,
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
	graph := invocationfixture.Graph(t)
	input := Input{
		Reference: graph.Entry().Reference, AvailableInputs: []rulemeta.InputKind{rulemeta.Invocations, rulemeta.Scripts}, CallGraph: graph,
		SelectedScripts: []invocation.ID{},
	}
	if err := input.ValidateInvocations(); err != nil {
		t.Fatalf("valid empty script collection rejected: %v", err)
	}
}
