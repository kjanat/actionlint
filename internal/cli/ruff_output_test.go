package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuffEnvironmentOutputRedirection(t *testing.T) {
	ruff, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("Ruff is not installed")
	}
	output := filepath.Join(t.TempDir(), "output.json")
	if err := os.WriteFile(output, []byte("preserve this file"), 0o600); err != nil {
		t.Fatal(err)
	}
	flags, err := json.Marshal([]string{"--output-file", output})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_RUFF_BIN", ruff)
	t.Setenv("ACTIONLINT_RUFF_FLAGS", string(flags))
	t.Setenv("ACTIONLINT_SHELLCHECK_BIN", "")
	source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print(missing)\n"
	for _, args := range [][]string{{"--no-config", "-"}, {"check", "--no-config", "-"}} {
		var stdout, stderr bytes.Buffer
		command := Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
		status := command.Main(append([]string{"actionlint", "--no-color"}, args...))
		if status == 0 || !strings.Contains(stdout.String()+stderr.String(), "output redirection") {
			t.Errorf("environment redirection not rejected: status=%d, stdout=%s, stderr=%s", status, &stdout, &stderr)
		}
		if content, err := os.ReadFile(output); err != nil || string(content) != "preserve this file" {
			t.Fatalf("environment output file modified: %q, %v", content, err)
		}
	}
}

func TestRuffEnvironmentStatistics(t *testing.T) {
	ruff, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("Ruff is not installed")
	}
	t.Setenv("ACTIONLINT_RUFF_BIN", ruff)
	t.Setenv("ACTIONLINT_RUFF_FLAGS", `["--statistics"]`)
	t.Setenv("ACTIONLINT_SHELLCHECK_BIN", "")
	source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print(missing)\n"
	for _, args := range [][]string{{"--no-config", "-"}, {"check", "--no-config", "-"}} {
		var stdout, stderr bytes.Buffer
		command := Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
		status := command.Main(append([]string{"actionlint", "--no-color"}, args...))
		if status == 0 || !strings.Contains(stdout.String()+stderr.String(), "statistics output is not supported") {
			t.Errorf("statistics not rejected: status=%d, stdout=%s, stderr=%s", status, &stdout, &stderr)
		}
	}
}

func TestRuffEnvironmentSilent(t *testing.T) {
	ruff, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("Ruff is not installed")
	}
	t.Setenv("ACTIONLINT_RUFF_BIN", ruff)
	t.Setenv("ACTIONLINT_SHELLCHECK_BIN", "")
	source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print(missing)\n"
	for _, flags := range []string{`["--silent"]`, `["-s"]`, `["-qs"]`} {
		t.Setenv("ACTIONLINT_RUFF_FLAGS", flags)
		for _, args := range [][]string{{"--no-config", "-"}, {"check", "--no-config", "-"}} {
			var stdout, stderr bytes.Buffer
			command := Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
			status := command.Main(append([]string{"actionlint", "--no-color"}, args...))
			if status == 0 || !strings.Contains(stdout.String()+stderr.String(), "silent output is not supported") {
				t.Errorf("silent mode not rejected: status=%d, stdout=%s, stderr=%s", status, &stdout, &stderr)
			}
		}
	}
}

func TestRuffEnvironmentIntegrationFlags(t *testing.T) {
	ruff, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("Ruff is not installed")
	}
	t.Setenv("ACTIONLINT_RUFF_BIN", ruff)
	t.Setenv("ACTIONLINT_SHELLCHECK_BIN", "")
	source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print(missing)\n"
	for _, flags := range []string{`["--no-cache"]`, `["--isolated"]`, `["--ignore-noqa"]`, `["--no-fix"]`, `["--target-version", "py314"]`, `["--target-version=py314"]`, `["--stdin-filename", "actionlint.py"]`, `["--stdin-filename=actionlint.py"]`} {
		t.Setenv("ACTIONLINT_RUFF_FLAGS", flags)
		for _, args := range [][]string{{"--no-config", "-"}, {"check", "--no-config", "-"}} {
			var stdout, stderr bytes.Buffer
			command := Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
			status := command.Main(append([]string{"actionlint", "--no-color"}, args...))
			if status == 0 || !strings.Contains(stdout.String()+stderr.String(), "integration-owned option") {
				t.Errorf("integration flag not rejected: flags=%s status=%d, stdout=%s, stderr=%s", flags, status, &stdout, &stderr)
			}
		}
	}
}
