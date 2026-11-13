package nosudo_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// These tests run the published actionlint CLI and check its JSON report.
func TestComponentThroughCLI(t *testing.T) {
	t.Parallel()
	binary := os.Getenv("ACTIONLINT_TEST_BINARY")
	if binary == "" {
		t.Skip("set ACTIONLINT_TEST_BINARY to an actionlint v2 executable; see ../README.md")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	component := os.Getenv("ACTIONLINT_TEST_COMPONENT")
	if component == "" {
		component = "../dist/plugin.wasm"
	}
	component, err = filepath.Abs(component)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(component); err != nil {
		t.Fatalf("build and package the component before testing; see ../README.md: %v", err)
	}
	for _, tc := range []struct {
		name, script string
		finding      bool
	}{
		{"ordinary command", "make install", false},
		{"argument", "echo sudo", false},
		{"longer command name", "sudoedit file", false},
		{"command", "sudo make install", true},
		{"assignment", `FOO="hello world" sudo make install`, true},
		{"unicode before command", "echo café; sudo make install", true},
		{"next line", "echo hello\nsudo make install", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			text := "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: |\n          " + strings.ReplaceAll(tc.script, "\n", "\n          ") + "\n"
			path := filepath.Join(root, "workflow.yml")
			if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			command := exec.CommandContext(t.Context(), binary, "check", "--no-config", "--no-tool", "shellcheck", "--no-tool", "ruff", "--json", "--plugin", component, "--", path)
			command.Dir = root
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			err := command.Run()
			var status *exec.ExitError
			if err != nil && (!errors.As(err, &status) || status.ExitCode() != 1) {
				t.Fatalf("actionlint: %v\n%s\n%s", err, &stderr, &stdout)
			}
			var report struct {
				Diagnostics []struct {
					Rule, Severity, Path string
					Start, End           struct{ Line, Column int }
					Reference            *struct{ Document, Revision string }
				}
			}
			if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
				t.Fatalf("JSON report: %v\n%s", err, &stdout)
			}
			if !tc.finding {
				if err != nil || len(report.Diagnostics) != 0 {
					t.Fatalf("clean script: status=%v report=%s", err, &stdout)
				}
				return
			}
			if err == nil || len(report.Diagnostics) != 1 {
				t.Fatalf("expected one finding and exit 1: status=%v report=%s", err, &stdout)
			}
			finding := report.Diagnostics[0]
			before, _, found := strings.Cut(text, "sudo")
			if !found {
				t.Fatal("workflow fixture does not contain sudo")
			}
			line := strings.Count(before, "\n") + 1
			column := utf8.RuneCountInString(before[strings.LastIndex(before, "\n")+1:]) + 1
			if finding.Rule != "no-sudo" || finding.Severity != "warning" || finding.Path != "workflow.yml" || finding.Start.Line != line || finding.Start.Column != column || finding.End.Line != line || finding.End.Column != column+4 || finding.Reference == nil || filepath.FromSlash(finding.Reference.Document) != path || finding.Reference.Revision == "" {
				t.Fatalf("finding lost its identity or source range: %s", &stdout)
			}
		})
	}
}
