package inputs

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/invocationfixture"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/rule"
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
	valid := func() AnalyzerInput {
		return AnalyzerInput{
			Reference: graph.Entry().Reference, AvailableInputs: []rule.InputKind{rule.Invocations, rule.Scripts}, CallGraph: graph,
			Scripts: []Script{rootText}, Invoked: []Script{invokedText},
			ActionInputs: []ActionInput{{Script: inputText, Site: ScriptInput{Action: "owner/action", Input: "script", Language: "javascript"}}},
		}
	}
	if err := valid().ValidateInvocations(); err != nil {
		t.Fatalf("valid action input rejected: %v", err)
	}
	for name, change := range map[string]func(*AnalyzerInput){
		"action input shell":    func(input *AnalyzerInput) { input.ActionInputs[0].Shell = "bash" },
		"action input runtime":  func(input *AnalyzerInput) { input.ActionInputs[0].Runtime = &scriptmodel.Execution{} },
		"action input action":   func(input *AnalyzerInput) { input.ActionInputs[0].Site.Action = "invalid" },
		"action input language": func(input *AnalyzerInput) { input.ActionInputs[0].Site.Language = "" },
		"entry run in invoked collection": func(input *AnalyzerInput) {
			input.Scripts = nil
			input.Invoked = []Script{rootText, invokedText}
		},
		"invoked run in scripts collection": func(input *AnalyzerInput) {
			input.Invoked = nil
			input.Scripts = []Script{rootText, invokedText}
		},
		"action input in scripts collection": func(input *AnalyzerInput) {
			input.ActionInputs = nil
			input.Scripts = []Script{rootText, inputText}
		},
		"action input in invoked collection": func(input *AnalyzerInput) {
			input.ActionInputs = nil
			input.Invoked = []Script{invokedText, inputText}
		},
		"action input selector mismatch": func(input *AnalyzerInput) {
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
	input := AnalyzerInput{
		Reference: graph.Entry().Reference, AvailableInputs: []rule.InputKind{rule.Invocations, rule.Scripts}, CallGraph: graph,
		SelectedScripts: []invocation.ID{},
	}
	if err := input.ValidateInvocations(); err != nil {
		t.Fatalf("valid empty script collection rejected: %v", err)
	}
}
