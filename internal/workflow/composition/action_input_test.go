package composition

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/analyzer"
)

func TestScriptInputCatalogueCombinesPerEngineLimits(t *testing.T) {
	var all []analyzer.ScriptInput
	for engine := range 2 {
		inputs := make([]analyzer.ScriptInput, 200)
		for i := range inputs {
			inputs[i] = analyzer.ScriptInput{Action: fmt.Sprintf("owner/engine-%d", engine), Input: fmt.Sprintf("input-%d", i), Language: "javascript"}
		}
		if err := analyzer.ValidateScriptInputs(inputs); err != nil {
			t.Fatal(err)
		}
		all = append(all, inputs...)
	}
	all = append(all, all[0])
	catalogue, err := scriptInputCatalogue(all)
	if err != nil || len(catalogue) != 401 {
		t.Fatalf("individually valid engines rejected: count=%d err=%v", len(catalogue), err)
	}
	invalid := append([]analyzer.ScriptInput(nil), all...)
	invalid = append(invalid, analyzer.ScriptInput{Action: "owner/action@v1", Input: "script", Language: "javascript"})
	if _, err := scriptInputCatalogue(invalid); err == nil {
		t.Fatal("invalid selector accepted in a combined catalogue")
	}
	conflict := all[0]
	conflict.Language = "python"
	if _, err := scriptInputCatalogue(append(all, conflict)); err == nil {
		t.Fatal("conflicting languages accepted in a combined catalogue")
	}
}

func TestScriptInputLanguagesMustAgree(t *testing.T) {
	javascript := analyzer.ScriptInput{Action: "owner/action", Input: "source", Language: "javascript"}
	python := javascript
	python.Language = "python"
	githubPython := analyzer.ScriptInput{Action: "actions/github-script", Input: "script", Language: "python"}
	for _, inputs := range [][]analyzer.ScriptInput{{javascript, python}, {githubPython}} {
		if _, err := scriptInputCatalogue(inputs); err == nil {
			t.Fatalf("conflicting language declarations accepted: %+v", inputs)
		}
	}
	catalogue, err := scriptInputCatalogue([]analyzer.ScriptInput{javascript, javascript})
	if err != nil || len(catalogue) != 2 {
		t.Fatalf("two engines agreeing on a site rejected: %+v err=%v", catalogue, err)
	}
}
