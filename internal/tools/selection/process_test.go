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
	cfg.Tools.Ruff.Backend = &toolconfig.Backend{Process: &toolconfig.Command{
		Executable: "mise", Args: []string{"exec", "ruff", "--", "ruff"}, Env: map[string]string{"CUSTOM": "literal $value"},
	}}
	automatic := &processconfig.Options{Optional: true, Arguments: []string{"--select", "F"}}
	selected := ProcessOptions("ruff", "ruff", automatic, cfg, "/analysis")
	if selected.Executable == nil || *selected.Executable != "mise" || !reflect.DeepEqual(selected.LauncherArguments, cfg.Tools.Ruff.Backend.Process.Args) || !reflect.DeepEqual(selected.Arguments, automatic.Arguments) || !reflect.DeepEqual(selected.Environment, []string{"CUSTOM=literal $value"}) {
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
