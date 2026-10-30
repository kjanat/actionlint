package selection

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/config"
	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
	"actionlint.kjanat.dev/v2/internal/processconfig"
)

func TestProjectToolBackendPrecedence(t *testing.T) {
	cfg := &config.Config{}
	cfg.Tools = config.ToolsConfig{"ruff": {Backend: &toolconfig.Backend{Process: &toolconfig.Command{
		Executable: "mise", Args: []string{"exec", "ruff", "--", "ruff"}, Env: map[string]string{"CUSTOM": "literal $value"},
	}}}}
	automatic := &processconfig.Options{Optional: true, Arguments: []string{"--select", "F"}}
	selected := ProcessOptions("ruff", "ruff", automatic, cfg, "/analysis")
	if selected.Executable == nil || *selected.Executable != "mise" || !reflect.DeepEqual(selected.LauncherArguments, cfg.Tools["ruff"].Backend.Process.Args) || !reflect.DeepEqual(selected.Arguments, automatic.Arguments) || !reflect.DeepEqual(selected.Environment, []string{"CUSTOM=literal $value"}) {
		t.Fatalf("wrong projection: %+v", selected)
	}
	explicit := &processconfig.Options{IgnoreConfig: true}
	if got := ProcessOptions("ruff", "ruff", explicit, cfg, "/analysis"); got != explicit {
		t.Fatal("explicit selection overridden")
	}
	if got := ProcessOptions("ruff", "", automatic, cfg, "/analysis"); got != automatic {
		t.Fatal("explicit disable overridden")
	}
}

func TestProvisionedProcessAllowsOnlyConfiguredWasm(t *testing.T) {
	for _, name := range []string{"ruff", "shellcheck"} {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{}
			backend := &toolconfig.Backend{Wasm: &toolconfig.Module{Path: "selected.wasm"}}
			if name == "ruff" {
				cfg.Tools = config.ToolsConfig{"ruff": {Backend: backend}}
			} else {
				cfg.Tools = config.ToolsConfig{"shellcheck": {Backend: backend}}
			}
			for _, tc := range []struct {
				name    string
				options *processconfig.Options
				want    bool
			}{
				{"automatic", &processconfig.Options{Optional: true}, true},
				{"explicit process", &processconfig.Options{}, false},
				{"provisioned process", &processconfig.Options{AllowWasm: true}, true},
				{"explicit override", &processconfig.Options{AllowWasm: true, IgnoreConfig: true}, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if got := Module(name, name, tc.options, cfg); (got != nil) != tc.want {
						t.Fatalf("selected=%+v want=%t", got, tc.want)
					}
				})
			}
			backend.Wasm = nil
			backend.Process = &toolconfig.Command{Executable: "untrusted"}
			if got := processCommand(name, name, &processconfig.Options{AllowWasm: true}, cfg); got != nil {
				t.Fatalf("repository process replaced provisioned command: %+v", got)
			}
		})
	}
}
