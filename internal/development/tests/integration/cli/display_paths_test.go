package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestDisplayPathInCommandOutput(t *testing.T) {
	dir := t.TempDir()
	working := filepath.Join(dir, "work")
	if err := os.Mkdir(working, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "ci.yml")
	if err := os.WriteFile(file, []byte(commandBadWorkflow), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(working)
	want := filepath.Join("..", "ci.yml")
	got := testRunCommandDefaults("", "--no-config", "-o", "pretty", "--diagnostic-width=400", "--summary", file)
	if got.Status != 1 || !strings.Contains(got.Stdout, "--> "+want+":6:24\n") || !strings.Contains(got.Stderr, "\n  1  "+want+"\n") {
		t.Fatalf("pretty output does not show %q: %+v", want, got)
	}
	got = testRunCommand("", "--no-config", "--json", file)
	var result report.CheckResult
	if err := json.Unmarshal([]byte(got.Stdout), &result); err != nil {
		t.Fatalf("invalid JSON: %v: %+v", err, got)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Path != want || len(result.Documents) != 1 || result.Documents[0].DocumentPath() != want {
		t.Fatalf("JSON paths = %+v, documents = %+v, want %q", result.Diagnostics, result.Documents, want)
	}
	got = testRunCommand(commandBadWorkflow, "check", "--no-config", "-o", "pretty", "--diagnostic-width=400", "--stdin-filename="+file, "-")
	if got.Status != 1 || !strings.Contains(got.Stdout, "--> "+want+":6:24\n") {
		t.Fatalf("stdin output does not show %q: %+v", want, got)
	}
}
