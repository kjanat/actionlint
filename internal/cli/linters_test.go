package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
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
			{"legacy value", "--json", []string{"-" + name, "--json", "input.yml"}},
			{"end of flags", name, []string{"--" + name, "--", "input.yml"}},
		} {
			t.Run(name+"/"+tc.label, func(t *testing.T) {
				app := newCommandApp(&Command{})
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

func TestLinterSwitches(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	t.Setenv("ACTIONLINT_RUFF_FLAGS", "[invalid")
	t.Setenv("ACTIONLINT_SHELLCHECK_FLAGS", "[invalid")
	custom := []string{"--ruff=" + strconv.Quote(executable), "--shellcheck=" + strconv.Quote(executable)}
	for _, tc := range []struct {
		name, status string
		flags        []string
	}{
		{"bare commands", "unavailable", []string{"--ruff", "--shellcheck"}},
		{"disabled", "disabled", []string{"--no-ruff", "--no-shellcheck"}},
		{"disable after command", "disabled", append(append([]string{}, custom...), "--no-ruff", "--no-shellcheck")},
		{"disable before command", "disabled", append([]string{"--no-ruff", "--no-shellcheck"}, custom...)},
		{"custom commands", "available", custom},
		{"explicit false", "available", append(append([]string{}, custom...), "--no-ruff=false", "--no-shellcheck=false")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			cmd := Command{Stdout: &out, Stderr: &stderr}
			args := append([]string{"actionlint", "doctor", "--no-config", "--json"}, tc.flags...)
			status := cmd.Main(args)
			var report struct {
				Tools []doctorTool `json:"tools"`
			}
			if err := json.Unmarshal(out.Bytes(), &report); err != nil || status != 0 || stderr.Len() != 0 || len(report.Tools) != 2 {
				t.Fatalf("exit=%d stdout=%s stderr=%s err=%v", status, &out, &stderr, err)
			}
			for _, tool := range report.Tools {
				if tool.Status != tc.status || tc.status == "disabled" && tool.Command != "" || tc.name == "bare commands" && tool.Command != tool.Name {
					t.Fatalf("unexpected linter setting: %+v, want %s", tool, tc.status)
				}
			}
		})
	}
}

func TestBareLinterFlagsLeaveWorkflowArguments(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("input.yml", []byte(commandGoodWorkflow), 0600); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range [][]string{nil, {"check"}} {
		var out, stderr bytes.Buffer
		cmd := Command{Stdout: &out, Stderr: &stderr}
		args := append([]string{"actionlint"}, prefix...)
		args = append(args, "--ruff", "--shellcheck", "--no-ruff", "--no-shellcheck", "--no-config", "--json", "--summary=false", "input.yml")
		if status := cmd.Main(args); status != 0 || stderr.Len() != 0 || !cleanCheckJSON(out.String()) {
			t.Fatalf("bare linter flag consumed a workflow argument: exit=%d stdout=%s stderr=%s", status, &out, &stderr)
		}
	}
}
