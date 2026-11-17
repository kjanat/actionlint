package backends

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/tools"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
)

func TestProjectToolBackendPrecedence(t *testing.T) {
	t.Parallel()
	cfg := &configuration.Config{}
	command, err := tools.NewCommand("mise", []string{"exec", "ruff", "--", "ruff"}, map[string]string{"CUSTOM": "literal $value"})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Tools = configuration.ToolsConfig{"ruff": {Backend: tools.ProcessBackend(command)}}
	automatic := &hostprocess.CommandOptions{Optional: true, Arguments: []string{"--select", "F"}}
	selected := ProcessOptions("ruff", "ruff", automatic, cfg, "/analysis")
	if selected.Executable == nil || *selected.Executable != "mise" || !reflect.DeepEqual(selected.LauncherArguments, command.Args()) || !reflect.DeepEqual(selected.Arguments, automatic.Arguments) || !reflect.DeepEqual(selected.Environment, []string{"CUSTOM=literal $value"}) {
		t.Fatalf("wrong projection: %+v", selected)
	}
	explicit := &hostprocess.CommandOptions{IgnoreConfig: true}
	if got := ProcessOptions("ruff", "ruff", explicit, cfg, "/analysis"); got != explicit {
		t.Fatal("explicit selection overridden")
	}
	if got := ProcessOptions("ruff", "", automatic, cfg, "/analysis"); got != automatic {
		t.Fatal("explicit disable overridden")
	}
}

func wasmToolConfig(t *testing.T, name string) *configuration.Config {
	t.Helper()
	module, err := tools.NewModule("selected.wasm")
	if err != nil {
		t.Fatal(err)
	}
	return &configuration.Config{Tools: configuration.ToolsConfig{name: {Backend: tools.WasmBackend(module)}}}
}

func TestProvisionedProcessAllowsOnlyConfiguredWasm(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"ruff", "shellcheck"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name    string
				options *hostprocess.CommandOptions
				want    bool
			}{
				{"automatic", &hostprocess.CommandOptions{Optional: true}, true},
				{"explicit process", &hostprocess.CommandOptions{}, false},
				{"provisioned process", &hostprocess.CommandOptions{AllowWasm: true}, true},
				{"explicit override", &hostprocess.CommandOptions{AllowWasm: true, IgnoreConfig: true}, false},
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
			command, err := tools.NewCommand("untrusted", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Tools[name] = configuration.ToolConfig{Backend: tools.ProcessBackend(command)}
			if got := processCommand(name, name, &hostprocess.CommandOptions{AllowWasm: true}, cfg); got != nil {
				t.Fatalf("repository process replaced provisioned command: %+v", got)
			}
		})
	}
}
