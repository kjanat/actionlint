package composition

import (
	"testing"

	"actionlint.kjanat.dev/analyzer"
)

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
