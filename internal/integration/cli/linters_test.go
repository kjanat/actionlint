package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
)

func TestLinterSwitches(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	t.Setenv("ACTIONLINT_TOOL_RUFF_FLAGS", "[invalid")
	t.Setenv("ACTIONLINT_TOOL_SHELLCHECK_FLAGS", "[invalid")
	custom := []string{"--tool=ruff=" + strconv.Quote(executable), "--tool=shellcheck=" + strconv.Quote(executable)}
	for _, tc := range []struct {
		name, status string
		flags        []string
	}{
		{"bare commands", "unavailable", []string{"--tool=ruff", "--tool=shellcheck"}},
		{"disabled", "disabled", []string{"--no-tool=ruff", "--no-tool=shellcheck"}},
		{"disable after command", "disabled", append(append([]string{}, custom...), "--no-tool=ruff", "--no-tool=shellcheck")},
		{"command after disable", "available", append([]string{"--no-tool=ruff", "--no-tool=shellcheck"}, custom...)},
		{"custom commands", "available", custom},
		{"explicit false", "available", append(append([]string{}, custom...), "--no-ruff=false", "--no-shellcheck=false")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			cmd := cli.Command{Stdout: &out, Stderr: &stderr}
			args := append([]string{"actionlint", "doctor", "--no-config", "--json"}, tc.flags...)
			status := cmd.Main(args)
			var report struct {
				Tools []doctorTool `json:"tools"`
			}
			if err := json.Unmarshal(out.Bytes(), &report); err != nil || status != 0 || !onlyDeprecationWarnings(stderr.String()) || len(report.Tools) != 2 {
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

func TestLinterCommandSpellings(t *testing.T) {
	t.Setenv("PATH", "")
	const toolCommand = `"/missing path/tool" -x -o all`
	for _, dash := range []string{"-", "--"} {
		for _, custom := range []bool{false, true} {
			for _, beforeCommand := range []bool{false, true} {
				args := []string{"actionlint", "--no-config", "--json"}
				if !beforeCommand {
					args = append(args, "doctor")
				}
				for _, name := range []string{"ruff", "shellcheck"} {
					if custom {
						args = append(args, dash+name+"="+toolCommand)
					} else {
						args = append(args, dash+name)
					}
				}
				if beforeCommand {
					args = append(args, "doctor")
				}
				var out, stderr bytes.Buffer
				cmd := cli.Command{Stdout: &out, Stderr: &stderr}
				status := cmd.Main(args)
				var report struct {
					Tools []doctorTool `json:"tools"`
				}
				if err := json.Unmarshal(out.Bytes(), &report); err != nil || status != 0 || !onlyDeprecationWarnings(stderr.String()) || len(report.Tools) != 2 {
					t.Fatalf("%v: exit=%d stdout=%s stderr=%s err=%v", args, status, &out, &stderr, err)
				}
				for _, tool := range report.Tools {
					want := tool.Name
					if custom {
						want = toolCommand
					}
					if tool.Command != want || tool.Status != "unavailable" {
						t.Fatalf("%v: command was not preserved: %+v", args, tool)
					}
				}
			}
		}
	}
}

func TestBareLinterFlagsLeaveWorkflowArguments(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("input.yml", []byte(commandGoodWorkflow), 0600); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range [][]string{nil, {"check"}} {
		for _, dash := range []string{"-", "--"} {
			var out, stderr bytes.Buffer
			cmd := cli.Command{Stdout: &out, Stderr: &stderr}
			args := append([]string{"actionlint"}, prefix...)
			args = append(args, dash+"ruff", dash+"shellcheck", "--no-tool=ruff", "--no-tool=shellcheck", "--no-config", "--json", "--summary=false", "input.yml")
			if status := cmd.Main(args); status != 0 || !onlyDeprecationWarnings(stderr.String()) || !cleanCheckJSON(out.String()) {
				t.Fatalf("%v: exit=%d stdout=%s stderr=%s", args, status, &out, &stderr)
			}
		}
	}
}

// onlyDeprecationWarnings reports whether stderr holds nothing but warnings
// about deprecated flags, as plain lines or JSON log records.
func onlyDeprecationWarnings(stderr string) bool {
	for line := range strings.SplitSeq(strings.TrimSpace(stderr), "\n") {
		if line != "" && !strings.Contains(line, "is deprecated; use --") {
			return false
		}
	}
	return true
}

func TestUnknownToolIDs(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--no-tool=missing"}, "unknown tool missing; valid tools: ruff, shellcheck"},
		{[]string{"--tool=missing"}, "unknown tool missing; valid tools: ruff, shellcheck"},
		{[]string{"--tool=missing=command"}, "only bundled tools take a command"},
		{[]string{"--no-tool=ruff=false"}, "is not a tool id"},
	} {
		got := testRunCommand(commandGoodWorkflow, append(tc.args, "-")...)
		if got.Status != 2 || !strings.Contains(got.Stderr, tc.want) {
			t.Errorf("%v: %+v, want status 2 and %q", tc.args, got, tc.want)
		}
	}
}

func TestToolEnvironmentAliases(t *testing.T) {
	t.Setenv("PATH", "")
	for _, tc := range []struct {
		name, command string
		env           map[string]string
		deprecated    bool
	}{
		{"new", "new-ruff", map[string]string{"ACTIONLINT_TOOL_RUFF_BIN": "new-ruff"}, false},
		{"former", "old-ruff", map[string]string{"ACTIONLINT_RUFF_BIN": "old-ruff"}, true},
		{"new wins", "new-ruff", map[string]string{"ACTIONLINT_TOOL_RUFF_BIN": "new-ruff", "ACTIONLINT_RUFF_BIN": "old-ruff"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{"ACTIONLINT_TOOL_RUFF_BIN", "ACTIONLINT_RUFF_BIN"} {
				t.Setenv(key, "")
				if err := os.Unsetenv(key); err != nil {
					t.Fatal(err)
				}
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			var out, stderr bytes.Buffer
			cmd := cli.Command{Stdout: &out, Stderr: &stderr}
			status := cmd.Main([]string{"actionlint", "doctor", "--no-config", "--json", "--no-tool=shellcheck"})
			var report struct {
				Tools []doctorTool `json:"tools"`
			}
			if err := json.Unmarshal(out.Bytes(), &report); err != nil || status != 0 {
				t.Fatalf("exit=%d stdout=%s stderr=%s err=%v", status, &out, &stderr, err)
			}
			if report.Tools[0].Name != "ruff" || report.Tools[0].Command != tc.command {
				t.Fatalf("environment selected %+v, want %s", report.Tools[0], tc.command)
			}
			if strings.Contains(stderr.String(), "ACTIONLINT_RUFF_BIN is deprecated; use ACTIONLINT_TOOL_RUFF_BIN") != tc.deprecated {
				t.Fatalf("deprecation warning: %s", &stderr)
			}
		})
	}
}
