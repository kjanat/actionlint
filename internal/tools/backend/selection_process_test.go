package backend

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/config"
	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
	"actionlint.kjanat.dev/v2/internal/processconfig"
)

func TestProjectToolBackendPrecedence(t *testing.T) {
	cfg := &config.Config{}
	command, err := toolconfig.NewCommand("mise", []string{"exec", "ruff", "--", "ruff"}, map[string]string{"CUSTOM": "literal $value"})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Tools = config.ToolsConfig{"ruff": {Backend: toolconfig.ProcessBackend(command)}}
	automatic := &processconfig.Options{Optional: true, Arguments: []string{"--select", "F"}}
	selected := ProcessOptions("ruff", "ruff", automatic, cfg, "/analysis")
	if selected.Executable == nil || *selected.Executable != "mise" || !reflect.DeepEqual(selected.LauncherArguments, command.Args()) || !reflect.DeepEqual(selected.Arguments, automatic.Arguments) || !reflect.DeepEqual(selected.Environment, []string{"CUSTOM=literal $value"}) {
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
			module, err := toolconfig.NewModule("selected.wasm")
			if err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{Tools: config.ToolsConfig{name: {Backend: toolconfig.WasmBackend(module)}}}
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
			command, err := toolconfig.NewCommand("untrusted", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Tools[name] = config.ToolConfig{Backend: toolconfig.ProcessBackend(command)}
			if got := processCommand(name, name, &processconfig.Options{AllowWasm: true}, cfg); got != nil {
				t.Fatalf("repository process replaced provisioned command: %+v", got)
			}
		})
	}
}
