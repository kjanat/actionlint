package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestRepositoryRuffCommandHelper(t *testing.T) {
	if os.Getenv("ACTIONLINT_REPO_RUFF_HELPER") != "1" {
		return
	}
	if err := os.WriteFile(os.Getenv("ACTIONLINT_REPO_RUFF_MARKER"), []byte("executed"), 0o600); err != nil {
		os.Exit(2)
	}
	// The bundled Ruff provider checks the minimum version before it checks
	// scripts.
	if slices.Contains(os.Args, "--version") {
		fmt.Print("ruff 0.17.0\n")
		os.Exit(0)
	}
	fmt.Print("[]")
	os.Exit(0)
}

func TestCLIRepositoryProcessOptIn(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, name := range []string{"ACTIONLINT_TOOL_RUFF_BIN", "ACTIONLINT_TOOL_RUFF_FLAGS", "ACTIONLINT_TOOL_RUFF_ENV"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "executed")
	config := "tools:\n  ruff:\n    backend:\n      process:\n        executable: " + strconv.Quote(executable) + "\n        args: ['-test.run=^TestRepositoryRuffCommandHelper$', '--']\n        env:\n          ACTIONLINT_REPO_RUFF_HELPER: '1'\n          ACTIONLINT_REPO_RUFF_MARKER: " + strconv.Quote(marker) + "\n"
	if err := os.WriteFile("actionlint.yaml", []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("ci.yml", []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print(1)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, allow := range []bool{false, true} {
		var stdout, stderr bytes.Buffer
		command := Command{Stdout: &stdout, Stderr: &stderr}
		args := []string{"actionlint", "check", "--config=actionlint.yaml", "--no-tool=shellcheck", "--summary=false", "--no-color", "ci.yml"}
		if allow {
			args = append(args, "--allow-config-processes")
		}
		status := command.Main(args)
		if !allow {
			if status != 3 || !strings.Contains(stderr.String(), "--allow-config-processes") {
				t.Fatalf("missing opt-in error: exit=%d stderr=%s", status, &stderr)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("untrusted configuration executed: %v", err)
			}
		} else if status != 0 {
			t.Fatalf("host opt-in failed: exit=%d stdout=%s stderr=%s", status, &stdout, &stderr)
		} else if _, err := os.Stat(marker); err != nil {
			t.Fatalf("approved process did not execute: %v", err)
		}
	}
}
