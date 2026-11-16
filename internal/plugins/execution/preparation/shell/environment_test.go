package shell

import (
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
)

func TestEnvironmentPreparationPreservesNamesAndSourceMapping(t *testing.T) {
	t.Parallel()
	environments := scripts.EnvironmentAlternatives("bash", []runnerapi.OS{runnerapi.Linux}, scripts.EnvironmentLayer{
		Scope: scripts.EnvironmentStep,
		Names: []string{"lowercase", "UPPERCASE", "miXedCase", "bad; command", "bad\ncommand", "1bad", "bad=value"},
	})
	prepared, err := BuildShellScriptInput(inputs.Script{
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
	if len(prepared.Lines) != 2 || prepared.Lines[0].Kind != inputs.ShellScriptGeneratedLine || prepared.Lines[1].Kind != inputs.ShellScriptSourceLine || prepared.Lines[1].Line != 1 {
		t.Fatalf("environment prelude changed source coordinates: %+v", prepared.Lines)
	}
}

func TestEnvironmentPreparationPreservesDeclaredNamesWithUnknownRuntime(t *testing.T) {
	t.Parallel()
	environments := scripts.EnvironmentAlternatives("bash", []runnerapi.OS{runnerapi.Linux},
		scripts.EnvironmentLayer{Scope: scripts.EnvironmentRuntime, UnknownNames: true},
		scripts.EnvironmentLayer{Scope: scripts.EnvironmentStep, Names: []string{"DECLARED"}},
	)
	startup, known := EnvironmentStartup("", environments)
	if startup != "export DECLARED" || known {
		t.Errorf("partial environment: startup=%q, known=%t", startup, known)
	}
	prepared, err := BuildShellScriptInput(inputs.Script{
		Text: `echo "$DECLARED" "$INHERITED"`, Shell: "bash", Environment: environments,
	}, nil)
	if err != nil || prepared == nil {
		t.Fatalf("prepare: %+v, %v", prepared, err)
	}
	if prepared.ShellSetup != "set -e -o pipefail; export DECLARED" {
		t.Errorf("declared name was not exported: %q", prepared.ShellSetup)
	}
	if len(prepared.Lines) != 2 || prepared.Lines[0].Kind != inputs.ShellScriptGeneratedLine || prepared.Lines[1].Kind != inputs.ShellScriptSourceLine || prepared.Lines[1].Line != 1 {
		t.Errorf("environment prelude changed source coordinates: %+v", prepared.Lines)
	}
}

func TestEnvironmentPreparationUsesAllOperatingSystemAlternatives(t *testing.T) {
	t.Parallel()
	layers := []scripts.EnvironmentLayer{
		{Scope: scripts.EnvironmentWorkflow, Names: []string{"foo"}},
		{Scope: scripts.EnvironmentStep, Names: []string{"FOO"}},
	}
	environments := scripts.EnvironmentAlternatives("bash", []runnerapi.OS{runnerapi.Linux, runnerapi.Windows}, layers...)
	startup, known := EnvironmentStartup("", environments)
	if startup != "export foo" || !known {
		t.Fatalf("Windows cannot export FOO after an earlier foo key: %q, %v", startup, known)
	}
	for _, environments := range [][]scripts.EnvironmentNames{
		nil,
		scripts.EnvironmentAlternatives("bash", nil, scripts.EnvironmentLayer{UnknownNames: true}),
	} {
		startup, known := EnvironmentStartup("set -e", environments)
		if startup != "set -e" || known {
			t.Fatalf("unresolved environment keys were treated as absent: %q, %v", startup, known)
		}
	}
}

func TestActionInputPreparationImportsEnvironmentNames(t *testing.T) {
	t.Parallel()
	environments := scripts.EnvironmentAlternatives("bash", []runnerapi.OS{runnerapi.Linux}, scripts.EnvironmentLayer{Scope: scripts.EnvironmentStep, Names: []string{"input_env"}})
	prepared, err := prepareShellcheck(t, Run{Input: true, Engine: true, Language: "bash", Environment: environments}, "echo \"$input_env\"")
	if err != nil || !strings.HasPrefix(prepared.Source, "export input_env\n") || slices.Contains(prepared.Excluded, "SC2154") {
		t.Fatalf("action input environment = %+v, %v", prepared, err)
	}
	if mapped := prepared.LineMap(2); mapped.Line != 1 {
		t.Fatalf("input source mapping = %+v", mapped)
	}
}
