package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
)

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
			cmd := cli.Command{Stdout: &out, Stderr: &stderr}
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
				if err := json.Unmarshal(out.Bytes(), &report); err != nil || status != 0 || stderr.Len() != 0 || len(report.Tools) != 2 {
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
			args = append(args, dash+"ruff", dash+"shellcheck", "--no-ruff", "--no-shellcheck", "--no-config", "--json", "--summary=false", "input.yml")
			if status := cmd.Main(args); status != 0 || stderr.Len() != 0 || !cleanCheckJSON(out.String()) {
				t.Fatalf("%v: exit=%d stdout=%s stderr=%s", args, status, &out, &stderr)
			}
		}
	}
}
