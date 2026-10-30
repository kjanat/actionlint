package cli

import (
	"reflect"
	"testing"
)

func TestRootOptionalLinterCommands(t *testing.T) {
	for _, name := range []string{"ruff", "shellcheck"} {
		for _, tc := range []struct {
			label, want string
			args        []string
		}{
			{"bare", name, []string{"--" + name, "input.yml"}},
			{"custom", "/path with spaces/tool --flag", []string{"--" + name + "=/path with spaces/tool --flag", "input.yml"}},
			{"empty", "", []string{"--" + name + "=", "input.yml"}},
			{"single dash bare", name, []string{"-" + name, "input.yml"}},
			{"end of flags", name, []string{"--" + name, "--", "input.yml"}},
			{"tool bare", name, []string{"--tool", name, "input.yml"}},
			{"tool custom", "/path with spaces/tool --flag", []string{"--tool=" + name + "=/path with spaces/tool --flag", "input.yml"}},
			{"tool false", "", []string{"--tool", name + "=false", "input.yml"}},
			{"no-tool", "", []string{"--no-tool", name, "input.yml"}},
			{"last no-tool wins", "", []string{"--tool", name, "--no-tool", name, "input.yml"}},
			{"last tool wins over no-tool", name, []string{"--no-" + name, "--tool", name, "input.yml"}},
			{"last tool wins", "last", []string{"--tool", name + "=first", "--" + name + "=last", "input.yml"}},
		} {
			t.Run(name+"/"+tc.label, func(t *testing.T) {
				app := newCommandApp(t.Context(), &Command{})
				rest, _, _, err := app.parseRoot(tc.args)
				var resolved resolvedTools
				if err == nil {
					resolved, err = app.inv.Check.resolveTools()
				}
				command := resolved.tools[name].Command
				if err != nil || command != tc.want || !reflect.DeepEqual(rest, []string{"input.yml"}) || app.errorJSON {
					t.Fatalf("command=%q rest=%v json=%v err=%v", command, rest, app.errorJSON, err)
				}
			})
		}
	}
}
