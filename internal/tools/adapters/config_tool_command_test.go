package adapters

import (
	"reflect"
	"testing"

	toolconfig "actionlint.kjanat.dev/internal/config/tools"
)

func TestProjectToolBackendPrecedence(t *testing.T) {
	cfg := &Config{}
	cfg.Tools.Ruff.Backend = &toolconfig.Backend{Process: &toolconfig.Command{
		Executable: "mise", Args: []string{"exec", "ruff", "--", "ruff"}, Env: map[string]string{"CUSTOM": "literal $value"},
	}}
	automatic := &ExternalCommandOptions{Optional: true, Arguments: []string{"--select", "F"}}
	selected := projectToolOptions("ruff", "ruff", automatic, cfg, "/analysis")
	if selected.Executable == nil || *selected.Executable != "mise" || !reflect.DeepEqual(selected.LauncherArguments, cfg.Tools.Ruff.Backend.Process.Args) || !reflect.DeepEqual(selected.Arguments, automatic.Arguments) || !reflect.DeepEqual(selected.Environment, []string{"CUSTOM=literal $value"}) {
		t.Fatalf("wrong projection: %+v", selected)
	}
	explicit := &ExternalCommandOptions{IgnoreConfig: true}
	if got := projectToolOptions("ruff", "ruff", explicit, cfg, "/analysis"); got != explicit {
		t.Fatal("explicit selection overridden")
	}
	if got := projectToolOptions("ruff", "", automatic, cfg, "/analysis"); got != automatic {
		t.Fatal("explicit disable overridden")
	}
}
