package process

import (
	"os"
	"strings"
	"testing"
)

func TestOutputLimitHelper(t *testing.T) {
	t.Parallel()
	mode := os.Getenv("ACTIONLINT_OUTPUT_LIMIT_HELPER")
	if mode == "" {
		return
	}
	stream := os.Stdout
	if mode == "stderr" {
		stream = os.Stderr
	}
	content := "ok"
	if mode != "small" {
		content = strings.Repeat("x", 64<<10)
	}
	if _, err := stream.WriteString(content); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestExecutionOutputLimit(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	coverageDirectory := t.TempDir()
	for _, mode := range []string{"stdout", "stderr", "small"} {
		for _, combined := range []bool{false, true} {
			execution := Execution{Cmd: executable, Args: []string{"-test.run=^TestOutputLimitHelper$"}, Env: []string{"ACTIONLINT_OUTPUT_LIMIT_HELPER=" + mode, "GOCOVERDIR=" + coverageDirectory}, OutputLimit: 1024, CombineOutput: combined}
			output, _, err := execution.RunStatus(t.Context())
			if mode == "small" {
				if err != nil || string(output) != "ok" {
					t.Fatalf("bounded clean execution: %q, %v", output, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "output exceeded 1024 bytes") {
				t.Fatalf("unbounded %s combined=%v: %v", mode, combined, err)
			}
		}
	}
}
