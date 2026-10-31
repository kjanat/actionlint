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
			{"tool bare", name, []string{"--tool", name, "input.yml"}},
			{"tool custom", "/path with spaces/tool --flag", []string{"--tool=" + name + "=/path with spaces/tool --flag", "input.yml"}},
			{"tool false", "", []string{"--tool", name + "=false", "input.yml"}},
			{"tool end of flags", name, []string{"--tool", name, "--", "input.yml"}},
			{"no-tool", "", []string{"--no-tool", name, "input.yml"}},
			{"last no-tool wins", "", []string{"--tool", name, "--no-tool", name, "input.yml"}},
			{"last tool wins over no-tool", name, []string{"--no-tool", name, "--tool", name, "input.yml"}},
			{"last tool wins", "last", []string{"--tool", name + "=first", "--tool", name + "=last", "input.yml"}},
		} {
			t.Run(name+"/"+tc.label, func(t *testing.T) {
				assertToolCommand(t, name, tc.args, tc.want)
			})
		}
	}
}

// TestRootDeprecatedShellCheckCommand checks the --shellcheck=COMMAND flag
// of actionlint v1.17.0, which the bundled ShellCheck manifest declares.
func TestRootDeprecatedShellCheckCommand(t *testing.T) {
	for _, tc := range []struct {
		label, want string
		args        []string
	}{
		{"custom", "/path with spaces/tool --flag", []string{"--shellcheck=/path with spaces/tool --flag", "input.yml"}},
		{"separate value", "shellcheck -o all", []string{"-shellcheck", "shellcheck -o all", "input.yml"}},
		{"empty", "", []string{"--shellcheck=", "input.yml"}},
		{"last tool wins", "last", []string{"--shellcheck=first", "--tool", "shellcheck=last", "input.yml"}},
		{"last deprecated flag wins", "last", []string{"--no-tool", "shellcheck", "--shellcheck=last", "input.yml"}},
	} {
		t.Run(tc.label, func(t *testing.T) {
			assertToolCommand(t, "shellcheck", tc.args, tc.want)
		})
	}
}

// assertToolCommand parses args and checks the command selected for the
// tool name and that input.yml is the only path left.
func assertToolCommand(t *testing.T, name string, args []string, want string) {
	t.Helper()
	app := newCommandApp(t.Context(), &Command{})
	rest, _, _, err := app.parseRoot(args)
	var resolved resolvedTools
	if err == nil {
		resolved, err = app.inv.Check.resolveTools()
	}
	command := resolved.tools[name].Command
	if err != nil || command != want || !reflect.DeepEqual(rest, []string{"input.yml"}) || app.errorJSON {
		t.Fatalf("command=%q rest=%v json=%v err=%v", command, rest, app.errorJSON, err)
	}
}
