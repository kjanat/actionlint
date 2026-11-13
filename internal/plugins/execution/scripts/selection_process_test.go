package scripts

import (
	"reflect"
	"testing"

	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	toolconfig "actionlint.kjanat.dev/v2/internal/input/configuration/tools"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
)

func TestProjectToolBackendPrecedence(t *testing.T) {
	t.Parallel()
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

func wasmToolConfig(t *testing.T, name string) *config.Config {
	t.Helper()
	module, err := toolconfig.NewModule("selected.wasm")
	if err != nil {
		t.Fatal(err)
	}
	return &config.Config{Tools: config.ToolsConfig{name: {Backend: toolconfig.WasmBackend(module)}}}
}

func TestProvisionedProcessAllowsOnlyConfiguredWasm(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"ruff", "shellcheck"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
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
					t.Parallel()
					cfg := wasmToolConfig(t, name)
					if got := Module(name, name, tc.options, cfg); (got != nil) != tc.want {
						t.Fatalf("selected=%+v want=%t", got, tc.want)
					}
				})
			}
			cfg := wasmToolConfig(t, name)
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
