package analyzer

import "testing"

func TestScriptInputSelectors(t *testing.T) {
	valid := ScriptInput{Action: "actions/github-script", Input: "script", Language: "javascript"}
	if err := ValidateScriptInputs([]ScriptInput{valid}); err != nil {
		t.Fatal(err)
	}
	for _, input := range []ScriptInput{
		{Action: "Actions/github-script", Input: "script", Language: "javascript"},
		{Action: "actions/github-script@v7", Input: "script", Language: "javascript"},
		{Action: "https://github.com/actions/github-script", Input: "script", Language: "javascript"},
		{Action: "./local", Input: "script", Language: "javascript"},
		{Action: "owner/repo/../script", Input: "script", Language: "javascript"},
		{Action: "owner/repo", Input: "", Language: "javascript"},
		{Action: "owner/repo", Input: "script", Language: "JavaScript"},
	} {
		if err := ValidateScriptInputs([]ScriptInput{input}); err == nil {
			t.Fatalf("invalid selector accepted: %+v", input)
		}
	}
	for _, language := range []string{"javascript", "python"} {
		other := valid
		other.Language = language
		if err := ValidateScriptInputs([]ScriptInput{valid, other}); err == nil {
			t.Fatalf("duplicate input target accepted with language %s", language)
		}
	}
	descriptor := ScriptEngineDescriptor{ScriptInputs: []ScriptInput{valid}}
	cloned := descriptor.Clone()
	cloned.ScriptInputs[0].Language = "python"
	if descriptor.ScriptInputs[0] != valid {
		t.Fatal("cloned input selectors alias descriptor")
	}
}
