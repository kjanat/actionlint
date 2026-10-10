package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
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
		command := Command{Stdout: &stdout, Stderr: &stderr}
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
			if tc.status == "unavailable" && !strings.Contains(text.Stdout, ruff.Error) {
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

func TestDoctorRuffProbeContext(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	request := doctorRequest{
		Config: actionlint.ConfigSelection{Disabled: true}, Ruff: "ignored",
		RuffOptions: &actionlint.ExternalCommandOptions{
			Executable: &executable, Optional: true, WorkingDir: root,
			Arguments: []string{"--select", "F"},
			Environment: []string{
				"ACTIONLINT_TEST_RUFF_DISCOVERY=1",
				"ACTIONLINT_TEST_RUFF_VERSION=ruff 0.17.0",
				"ACTIONLINT_TEST_RUFF_CALLS=calls",
			},
		}, JSON: true,
	}
	var out bytes.Buffer
	if err := writeDoctor(t.Context(), &out, request); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Tools []doctorTool `json:"tools"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if tool := report.Tools[1]; tool.Status != "available" || strings.Join(tool.Arguments, " ") != "--select F" {
		t.Fatalf("configured tool changed: %+v", tool)
	}
	calls, err := os.ReadFile(filepath.Join(root, "calls"))
	if err != nil || string(calls) != "--version\n" {
		t.Fatalf("child directory/environment lost: %q, %v", calls, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	out.Reset()
	if err := writeDoctor(ctx, &out, request); !errors.Is(err, context.Canceled) || out.Len() != 0 {
		t.Fatalf("cancellation became a tool status: %s, %v", &out, err)
	}
	calls, err = os.ReadFile(filepath.Join(root, "calls"))
	if err != nil || string(calls) != "--version\n" {
		t.Fatalf("canceled probe executed: %q, %v", calls, err)
	}
}
