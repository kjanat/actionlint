package composites

import (
	"slices"
	"testing"

	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
)

func TestScriptEnvironmentScopesAndInvocationIsolation(t *testing.T) {
	t.Parallel()
	workflow := `on: push
env: { lowercase: lower, UPPERCASE: upper, miXedCase: mixed }
jobs:
  linux:
    runs-on: ubuntu-latest
    env: { job_name: job }
    steps:
      - uses: $/child
        env: { caller_name: caller }
      - shell: bash
        run: echo sibling
  windows:
    runs-on: windows-latest
    env: { LOWERCASE: override }
    steps:
      - shell: bash
        run: echo bash
      - shell: pwsh
        run: Write-Output $env:LOWERCASE
`
	f := newInvocationFixture(t, t.Context(), workflow, map[string]string{
		"child/action.yml": `name: child
description: child
runs:
  using: composite
  steps:
    - shell: bash
      env: { child_name: child }
      run: echo child
`,
	}, nil)
	result, err := Workflow(f.options, f.root)
	if err != nil {
		t.Fatal(err)
	}
	units := result.Batch.Scripts()
	if len(units) != 4 {
		t.Fatalf("scripts = %d, want 4", len(units))
	}
	for index, unit := range units {
		fact, ok := result.Batch.Fact(index, false)
		if !ok || fact.Runtime != nil || len(fact.Environment) == 0 {
			t.Fatalf("environment facts require neither directory resolution nor the execution input: %+v", fact)
		}
		for _, environment := range fact.Environment {
			if !environment.UnknownNames() {
				t.Error("inherited runner variables were treated as fully enumerated")
			}
			for _, name := range []string{"lowercase", "UPPERCASE", "miXedCase"} {
				want := scriptmodel.EnvironmentDeclared
				if environment.OperatingSystem() == runnerapi.Windows && environment.LookupComparison() == scriptmodel.NameSensitive {
					want = scriptmodel.EnvironmentUncertain
				}
				if environment.Lookup(name) != want {
					t.Errorf("script %q on %s lost %q", unit.Run.Run().Value(), environment.OperatingSystem(), name)
				}
			}
			switch unit.Run.Run().Value() {
			case "echo child":
				for _, name := range []string{"job_name", "caller_name", "child_name"} {
					if environment.Lookup(name) != scriptmodel.EnvironmentDeclared {
						t.Errorf("composite lost %q: %+v", name, environment.Names())
					}
				}
			case "echo sibling":
				for _, name := range []string{"caller_name", "child_name"} {
					if environment.Lookup(name) != scriptmodel.EnvironmentUncertain || slices.ContainsFunc(environment.Names(), func(variable scriptmodel.EnvironmentVariableName) bool { return variable.Name == name }) {
						t.Errorf("composite scope leaked %q to sibling", name)
					}
				}
			case "echo bash":
				if environment.OperatingSystem() != runnerapi.Windows || environment.LookupComparison() != scriptmodel.NameSensitive || environment.Lookup("LOWERCASE") != scriptmodel.EnvironmentUncertain {
					t.Errorf("Windows runner merging must not make Bash lookup insensitive: %+v", environment)
				}
			case "Write-Output $env:LOWERCASE":
				if environment.OperatingSystem() != runnerapi.Windows || environment.Lookup("LOWERCASE") != scriptmodel.EnvironmentDeclared {
					t.Errorf("Windows PowerShell lookup should ignore casing: %+v", environment)
				}
			}
		}
	}
}
