package scripts

import (
	"slices"
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	mapping "actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
)

func TestDeclaredEnvironmentPreservesSourceOrderAndCase(t *testing.T) {
	t.Parallel()
	declarations := mapping.Resolve(&mapping.Inputs{Values: map[string]mapping.Input{
		"foo": {Name: value.YAMLScalar("foo", "!!str", value.Origin{Span: value.Span{Start: sourcepos.Position{Line: 2, Column: 3}}})},
		"FOO": {Name: value.YAMLScalar("FOO", "!!str", value.Origin{Span: value.Span{Start: sourcepos.Position{Line: 3, Column: 3}}})},
	}})
	layer := DeclaredEnvironment(EnvironmentStep, declarations)
	if layer.UnknownNames || !slices.Equal(layer.Names, []string{"foo", "FOO"}) {
		t.Fatalf("declarations lost spelling or source order: %+v", layer)
	}
	environment := EnvironmentAlternatives("bash", []runnerapi.OS{runnerapi.Windows}, layer)[0]
	if environment.Lookup("foo") != EnvironmentDeclared || environment.Lookup("FOO") != EnvironmentUndeclared {
		t.Fatalf("map iteration order changed the runner's exported spelling: %+v", environment.Names())
	}
}

func TestEnvironmentNamesSeparateMergeFromLookup(t *testing.T) {
	t.Parallel()
	layers := []EnvironmentLayer{
		{Scope: EnvironmentWorkflow, Names: []string{"lowercase", "UPPERCASE", "miXedCase"}},
		{Scope: EnvironmentJob, Names: []string{"LOWERCASE"}},
		{Scope: EnvironmentStep, Names: []string{"mixedcase"}},
	}
	for _, tc := range []struct {
		name, shell string
		os          runnerapi.OS
		wantNames   []string
		wantLookup  EnvironmentNamePresence
	}{
		{"Linux bash", "bash", runnerapi.Linux, []string{"lowercase", "UPPERCASE", "miXedCase", "LOWERCASE", "mixedcase"}, EnvironmentUndeclared},
		{"Windows bash", "bash", runnerapi.Windows, []string{"lowercase", "UPPERCASE", "miXedCase"}, EnvironmentUndeclared},
		{"Windows PowerShell", "pwsh", runnerapi.Windows, []string{"lowercase", "UPPERCASE", "miXedCase"}, EnvironmentDeclared},
		{"Linux PowerShell", "pwsh", runnerapi.Linux, []string{"lowercase", "UPPERCASE", "miXedCase", "LOWERCASE", "mixedcase"}, EnvironmentUndeclared},
		{"macOS sh", "sh", runnerapi.MacOS, []string{"lowercase", "UPPERCASE", "miXedCase", "LOWERCASE", "mixedcase"}, EnvironmentUndeclared},
		{"Windows cmd", "cmd", runnerapi.Windows, []string{"lowercase", "UPPERCASE", "miXedCase"}, EnvironmentDeclared},
		{"Windows Bash path", `C:\ProgramFiles\Git\bin\bash.exe --noprofile {0}`, runnerapi.Windows, []string{"lowercase", "UPPERCASE", "miXedCase"}, EnvironmentUndeclared},
		{"PowerShell template", "pwsh -command . '{0}'", runnerapi.Windows, []string{"lowercase", "UPPERCASE", "miXedCase"}, EnvironmentDeclared},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			environment := EnvironmentAlternatives(tc.shell, []runnerapi.OS{tc.os}, layers...)[0]
			var names []string
			for _, variable := range environment.Names() {
				names = append(names, variable.Name)
			}
			if !slices.Equal(names, tc.wantNames) {
				t.Fatalf("exported names=%v, want %v", names, tc.wantNames)
			}
			if got := environment.Lookup("uppercase"); got != tc.wantLookup {
				t.Fatalf("uppercase lookup=%s, want %s", got, tc.wantLookup)
			}
			if environment.Lookup("lowercase") != EnvironmentDeclared || environment.Lookup("unrelated") != EnvironmentUndeclared {
				t.Fatal("lost declared names or invented an unrelated declaration")
			}
		})
	}
}

func TestWindowsBashUsesFirstScopeSpelling(t *testing.T) {
	t.Parallel()
	environment := EnvironmentAlternatives("bash", []runnerapi.OS{runnerapi.Windows},
		EnvironmentLayer{Scope: EnvironmentWorkflow, Names: []string{"foo"}},
		EnvironmentLayer{Scope: EnvironmentJob, Names: []string{"FOO"}},
		EnvironmentLayer{Scope: EnvironmentCaller, Names: []string{"Foo"}},
		EnvironmentLayer{Scope: EnvironmentStep, Names: []string{"fOo"}},
	)[0]
	if environment.Lookup("foo") != EnvironmentDeclared || environment.Lookup("FOO") != EnvironmentUndeclared {
		t.Fatalf("runner merging was confused with Bash lookup: %+v", environment.Names())
	}
	if names := environment.Names(); len(names) != 1 || names[0].Scope != EnvironmentWorkflow || !names[0].CaseKnown {
		t.Fatalf("lost the spelling's introducing scope: %+v", names)
	}
}

func TestUnknownEnvironmentDeclarationsDoNotInventWindowsSpelling(t *testing.T) {
	t.Parallel()
	layers := []EnvironmentLayer{
		{Scope: EnvironmentWorkflow, Names: []string{"EXISTING"}},
		{Scope: EnvironmentCaller, UnknownNames: true},
		{Scope: EnvironmentStep, Names: []string{"declared"}},
	}
	for _, shell := range []string{"bash", "pwsh"} {
		environment := EnvironmentAlternatives(shell, []runnerapi.OS{runnerapi.Windows}, layers...)[0]
		want := EnvironmentUncertain
		if shell == "pwsh" {
			want = EnvironmentDeclared
		}
		if environment.Lookup("declared") != want || environment.Lookup("EXISTING") != EnvironmentDeclared || environment.Lookup("unresolved") != EnvironmentUncertain {
			t.Fatalf("%s discarded uncertainty or established names: %+v", shell, environment)
		}
	}
	linux := EnvironmentAlternatives("bash", []runnerapi.OS{runnerapi.Linux}, layers...)[0]
	if linux.Lookup("declared") != EnvironmentDeclared {
		t.Fatal("unknown outer declarations erased an exact Linux name")
	}
}

func TestEnvironmentAlternativesOwnTheirNames(t *testing.T) {
	t.Parallel()
	names := []string{"original"}
	alternatives := EnvironmentAlternatives("bash", nil, EnvironmentLayer{Scope: EnvironmentStep, Names: names})
	names[0] = "mutated"
	if len(alternatives) != 3 {
		t.Fatalf("unknown OS produced %d alternatives", len(alternatives))
	}
	for _, environment := range alternatives {
		returned := environment.Names()
		returned[0].Name = "mutated"
		if environment.Lookup("original") != EnvironmentDeclared || environment.Lookup("mutated") != EnvironmentUndeclared {
			t.Fatal("a caller changed captured environment names")
		}
	}
}

func TestWindowsEnvironmentUsesRunnerOrdinalComparison(t *testing.T) {
	t.Parallel()
	environment := EnvironmentAlternatives("pwsh", []runnerapi.OS{runnerapi.Windows}, EnvironmentLayer{Names: []string{"I", "S"}})[0]
	for _, name := range []string{"ı", "ſ"} {
		if environment.Lookup(name) != EnvironmentUndeclared {
			t.Fatalf("Unicode case folding invented environment name %q", name)
		}
	}
}
