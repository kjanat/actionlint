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
		} {
			t.Run(name+"/"+tc.label, func(t *testing.T) {
				app := newCommandApp(t.Context(), &Command{})
				rest, _, _, err := app.parseRoot(tc.args)
				command := app.inv.Check.Ruff
				if name == "shellcheck" {
					command = app.inv.Check.ShellCheck
				}
				if err != nil || command != tc.want || !reflect.DeepEqual(rest, []string{"input.yml"}) || app.errorJSON {
					t.Fatalf("command=%q rest=%v json=%v err=%v", command, rest, app.errorJSON, err)
				}
			})
		}
	}
}
