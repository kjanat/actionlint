package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
)

func TestDoctorRuffCompatibility(t *testing.T) {
	unavailable := regexp.MustCompile(`(?m)^ruff:[ \t]+.+ \(unavailable: .+\)$`)
	name := "ruff"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	// Probe a small native executable: copying this package's race test binary
	// makes first startup compete with the production probe's five-second limit.
	probe := filepath.Join(t.TempDir(), name)
	build := exec.CommandContext(t.Context(), "go", "build", "-race=false", "-o", probe, "./internal/development/tests/integration/cli/testdata/ruff-probe")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Ruff probe fixture: %v: %s", err, output)
	}
	executable, err := os.ReadFile(probe)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	for _, key := range []string{"ACTIONLINT_TOOL_RUFF_BIN", "ACTIONLINT_TOOL_RUFF_FLAGS", "ACTIONLINT_TOOL_RUFF_ENV"} {
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
		name, version, status, reason string
		explicit                      bool
		args                          []string
		probe                         bool
	}{
		{"old", "ruff 0.16.10", "unavailable", "is incompatible; requires 0.17.0 or newer", false, nil, true},
		{"current", "ruff 0.17.0", "available", "", false, nil, true},
		{"invalid", "unexpected banner", "unavailable", "could not identify ruff version", false, nil, true},
		{"explicit", "ruff 0.16.10", "available", "", true, nil, false},
		{"disabled", "ruff 0.16.10", "disabled", "", false, []string{"--tool=ruff=false"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			tool := filepath.Join(bin, name)
			if err := os.WriteFile(tool, executable, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			// The probe runs with a scrubbed environment; the fake reads its
			// version from a file beside it.
			calls := testutil.RuffDiscoveryFixture(t, bin, tc.version)
			args := append([]string{"doctor", "--no-config", "--tool=shellcheck=false"}, tc.args...)
			if tc.explicit {
				args = append(args, "--tool=ruff="+tool)
			}
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
			ruff := report.Tools[slices.IndexFunc(report.Tools, func(tool doctorTool) bool { return tool.Name == "ruff" })]
			if ruff.Status != tc.status || (ruff.Error != "") != (tc.status == "unavailable") || !strings.Contains(ruff.Error, tc.reason) {
				t.Fatalf("incorrect compatibility report: %+v", ruff)
			}
			text := run(args...)
			if text.Status != 0 || text.Stderr != "" {
				t.Fatalf("text doctor failed: %+v", text)
			}
			if tc.status == "unavailable" && (!unavailable.MatchString(text.Stdout) || !strings.Contains(text.Stdout, tc.reason)) {
				t.Fatalf("text hid compatibility failure: %+v", text)
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
