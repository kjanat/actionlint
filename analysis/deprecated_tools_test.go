package analysis

import "testing"

// TestDeprecatedToolFieldsAreAliases checks that the deprecated ShellCheck
// and Ruff fields select their tool unless Tools has the same key.
func TestDeprecatedToolFieldsAreAliases(t *testing.T) {
	alias := Tool{Process: &Process{Executable: "alias"}}
	current := Tool{Process: &Process{Executable: "current"}}
	tools := selectedTools(Options{ShellCheck: alias, Ruff: alias, Tools: map[string]Tool{"ruff": current}})
	if got := tools["shellcheck"].Process; got == nil || got.Executable != "alias" {
		t.Fatalf("ShellCheck field ignored: %+v", tools["shellcheck"])
	}
	if got := tools["ruff"].Process; got == nil || got.Executable != "current" {
		t.Fatalf("Tools[\"ruff\"] did not win over the Ruff field: %+v", tools["ruff"])
	}
}
