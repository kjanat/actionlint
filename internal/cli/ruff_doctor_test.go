package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
)

func TestDoctorRuffProbeContext(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	request := doctorRequest{
		Config: repoconfig.Selection{Disabled: true}, Ruff: "ignored",
		RuffOptions: &processconfig.Options{
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
