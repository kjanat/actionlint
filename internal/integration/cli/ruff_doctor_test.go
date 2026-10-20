package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/cli"
)

func TestDoctorRuffCompatibility(t *testing.T) {
	t.Chdir(t.TempDir())
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	tool := filepath.Join(bin, "ruff")
	if runtime.GOOS == "windows" {
		tool += ".exe"
	}
	if err := os.WriteFile(tool, data, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("ACTIONLINT_TEST_RUFF_DISCOVERY", "1")
	for _, key := range []string{"ACTIONLINT_RUFF_BIN", "ACTIONLINT_RUFF_FLAGS", "ACTIONLINT_RUFF_ENV"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) commandTranscript {
		var stdout, stderr bytes.Buffer
		command := cli.Command{Stdout: &stdout, Stderr: &stderr}
		status := command.Main(append([]string{"actionlint", "--no-color"}, args...))
		return commandTranscript{status, stdout.String(), stderr.String()}
	}
	for _, tc := range []struct {
		name, version, status string
		args                  []string
		probe                 bool
	}{
		{"old", "ruff 0.16.10", "unavailable", nil, true},
		{"current", "ruff 0.17.0", "available", nil, true},
		{"invalid", "unexpected banner", "unavailable", nil, true},
		{"explicit", "ruff 0.16.10", "available", []string{"--ruff=" + tool}, false},
		{"disabled", "ruff 0.16.10", "disabled", []string{"--ruff=false"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := filepath.Join(t.TempDir(), "calls")
			t.Setenv("ACTIONLINT_TEST_RUFF_CALLS", calls)
			t.Setenv("ACTIONLINT_TEST_RUFF_VERSION", tc.version)
			args := append([]string{"doctor", "--no-config", "--shellcheck=false"}, tc.args...)
			result := run(append(args, "--json")...)
			if result.Status != 0 || result.Stderr != "" {
				t.Fatalf("doctor failed: %+v", result)
			}
			var report struct {
				Tools []doctorTool `json:"tools"`
			}
			if err := json.Unmarshal([]byte(result.Stdout), &report); err != nil {
				t.Fatal(err)
			}
			ruff := report.Tools[1]
			if ruff.Status != tc.status || (ruff.Error != "") != (tc.status == "unavailable") {
				t.Fatalf("incorrect compatibility report: %+v", ruff)
			}
			text := run(args...)
			if text.Status != 0 || text.Stderr != "" {
				t.Fatalf("text doctor failed: %+v", text)
			}
			if tc.status == "unavailable" && !strings.Contains(text.Stdout, ruff.Error) {
				t.Fatalf("text hid JSON compatibility failure %q: %+v", ruff.Error, text)
			}
			invocations, err := os.ReadFile(calls)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			want := ""
			if tc.probe {
				want = "--version\n--version\n"
			}
			if string(invocations) != want {
				t.Fatalf("unexpected probe calls: %q", invocations)
			}
		})
	}
}
