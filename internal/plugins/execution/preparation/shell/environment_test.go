package shell

import (
	"slices"
	"strings"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
)

func TestEnvironmentPreparationPreservesNamesAndSourceMapping(t *testing.T) {
	environments := scriptmodel.EnvironmentAlternatives("bash", []runnerapi.OS{runnerapi.Linux}, scriptmodel.EnvironmentLayer{
		Scope: scriptmodel.EnvironmentStep,
		Names: []string{"lowercase", "UPPERCASE", "miXedCase", "bad; command", "bad\ncommand", "1bad", "bad=value"},
	})
	prepared, err := BuildShellScriptInput(checkinputs.Script{
		Text: `printf '%s\n' "$lowercase" "$UPPERCASE" "$miXedCase" "$missing_lowercase"`, Shell: "bash",
		Environment: environments,
	}, nil)
	if err != nil || prepared == nil {
		t.Fatalf("prepare: %+v, %v", prepared, err)
	}
	if prepared.ShellSetup != "set -e -o pipefail; export lowercase UPPERCASE miXedCase" {
		t.Fatalf("preparation assigned values, normalized names, or injected invalid keys: %q", prepared.ShellSetup)
	}
	if slices.Contains(prepared.Excluded, uint32(2153)) || slices.Contains(prepared.Excluded, uint32(2154)) {
		t.Fatal("known environment declarations disabled unassigned-variable checks")
	}
	if len(prepared.Lines) != 2 || prepared.Lines[0].Kind != checkinputs.ShellScriptGeneratedLine || prepared.Lines[1].Kind != checkinputs.ShellScriptSourceLine || prepared.Lines[1].Line != 1 {
		t.Fatalf("environment prelude changed source coordinates: %+v", prepared.Lines)
	}
}

func TestEnvironmentPreparationUsesAllOperatingSystemAlternatives(t *testing.T) {
	layers := []scriptmodel.EnvironmentLayer{
		{Scope: scriptmodel.EnvironmentWorkflow, Names: []string{"foo"}},
		{Scope: scriptmodel.EnvironmentStep, Names: []string{"FOO"}},
	}
	environments := scriptmodel.EnvironmentAlternatives("bash", []runnerapi.OS{runnerapi.Linux, runnerapi.Windows}, layers...)
	startup, excluded := environmentStartup("", environments)
	if startup != "export foo" || len(excluded) != 0 {
		t.Fatalf("Windows cannot export FOO after an earlier foo key: %q, %v", startup, excluded)
	}
	for _, environments := range [][]scriptmodel.EnvironmentNames{
		nil,
		scriptmodel.EnvironmentAlternatives("bash", nil, scriptmodel.EnvironmentLayer{UnknownNames: true}),
	} {
		startup, excluded := environmentStartup("set -e", environments)
		if startup != "set -e" || !slices.Contains(excluded, "SC2153") || !slices.Contains(excluded, "SC2154") {
			t.Fatalf("unresolved environment keys were treated as absent: %q, %v", startup, excluded)
		}
	}
}

func TestActionInputPreparationImportsEnvironmentNames(t *testing.T) {
	environments := scriptmodel.EnvironmentAlternatives("bash", []runnerapi.OS{runnerapi.Linux}, scriptmodel.EnvironmentLayer{Scope: scriptmodel.EnvironmentStep, Names: []string{"input_env"}})
	prepared, err := EngineInput("echo \"$input_env\"", "bash", nil, environments)
	if err != nil || !strings.HasPrefix(prepared.Source, "export input_env\n") || slices.Contains(prepared.Excluded, "SC2154") {
		t.Fatalf("action input environment = %+v, %v", prepared, err)
	}
	if mapped := prepared.LineMap(2); mapped.Line != 1 {
		t.Fatalf("input source mapping = %+v", mapped)
	}
}
