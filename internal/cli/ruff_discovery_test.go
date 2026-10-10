package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func ruffDiscoveryHelper() int {
	file, err := os.OpenFile(os.Getenv("ACTIONLINT_TEST_RUFF_CALLS"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 3
	}
	_, writeErr := fmt.Fprintln(file, strings.Join(os.Args[1:], " "))
	if err := file.Close(); err != nil || writeErr != nil {
		return 3
	}
	version := os.Getenv("ACTIONLINT_TEST_RUFF_VERSION")
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(version)
		return 0
	}
	if version != "ruff 0.17.0" {
		fmt.Fprintln(os.Stderr, "unsupported target-version: requires newer Ruff")
		return 2
	}
	fmt.Println("[]")
	return 0
}

func TestRuffAutomaticVersionCompatibility(t *testing.T) {
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
	python := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(1)
`
	for _, prefix := range [][]string{nil, {"check"}} {
		for _, tc := range []struct {
			name, version, config, source, environment string
			explicit, warning, probe                   bool
			status                                     int
		}{
			{name: "old default", version: "ruff 0.9.0", source: python, warning: true, probe: true},
			{name: "old py315", version: "ruff 0.16.0", config: "tools: {ruff: {target-version: py315}}", source: python, warning: true, probe: true},
			{name: "unrecognized", version: "unexpected banner", source: python, warning: true, probe: true},
			{name: "current py315", version: "ruff 0.17.0", config: "tools: {ruff: {target-version: py315}}", source: python, probe: true},
			{name: "explicit old", version: "ruff 0.9.0", source: python, explicit: true, status: 3},
			{name: "environment binary old", version: "ruff 0.9.0", source: python, environment: "BIN", status: 3},
			{name: "environment flags old", version: "ruff 0.9.0", source: python, environment: "FLAGS", status: 3},
			{name: "non python", version: "ruff 0.9.0", source: commandGoodWorkflow},
			{name: "tool disabled", version: "ruff 0.9.0", source: python, config: "tools: {ruff: false}"},
			{name: "rule disabled", version: "ruff 0.9.0", source: python, config: "lint: {rules: {external: {ruff: off}}}"},
			{name: "independent findings", version: "ruff 0.9.0", source: python + "      - run: echo '${{ missing.value }}'\n", warning: true, probe: true, status: 1},
		} {
			t.Run(fmt.Sprint(prefix, tc.name), func(t *testing.T) {
				calls := filepath.Join(t.TempDir(), "calls")
				t.Setenv("ACTIONLINT_TEST_RUFF_CALLS", calls)
				t.Setenv("ACTIONLINT_TEST_RUFF_VERSION", tc.version)
				switch tc.environment {
				case "BIN":
					t.Setenv("ACTIONLINT_RUFF_BIN", tool)
				case "FLAGS":
					t.Setenv("ACTIONLINT_RUFF_FLAGS", `["--select", "F"]`)
				}
				args := append([]string{"actionlint", "--no-color", "--shellcheck="}, prefix...)
				if tc.config == "" {
					args = append(args, "--no-config")
				} else {
					config := filepath.Join(t.TempDir(), "config.yaml")
					if err := os.WriteFile(config, []byte(tc.config), 0o600); err != nil {
						t.Fatal(err)
					}
					args = append(args, "--config-file", config)
				}
				if tc.explicit {
					args = append(args, "--ruff="+tool)
				}
				// Multiple source files must share the version probe and warning.
				for index := range 2 {
					path := filepath.Join(t.TempDir(), fmt.Sprintf("workflow%d.yaml", index))
					if err := os.WriteFile(path, []byte(tc.source), 0o600); err != nil {
						t.Fatal(err)
					}
					args = append(args, path)
				}
				var stdout, stderr bytes.Buffer
				command := Command{Stdout: &stdout, Stderr: &stderr}
				status := command.Main(args)
				if status != tc.status || strings.Contains(stderr.String(), "warning: skipping automatically discovered Ruff") != tc.warning {
					t.Fatalf("status=%d stdout=%s stderr=%s", status, &stdout, &stderr)
				}
				if tc.warning && strings.Count(stderr.String(), "warning: skipping automatically discovered Ruff") != 1 {
					t.Fatalf("warning repeated: %s", &stderr)
				}
				invocations, err := os.ReadFile(calls)
				if err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				wantProbes := 0
				if tc.probe {
					wantProbes = 1
				}
				if strings.Count(string(invocations), "--version") != wantProbes || tc.warning && strings.Contains(string(invocations), "check") {
					t.Fatalf("unexpected probe/check invocations: %s", invocations)
				}
				if tc.version == "ruff 0.17.0" && (strings.Count(string(invocations), "check ") != 2 || strings.Count(string(invocations), "--target-version py315") != 2) {
					t.Fatalf("compatible checker did not check both py315 scripts: %s", invocations)
				}
				if tc.name == "independent findings" && !strings.Contains(stdout.String(), "missing") {
					t.Fatalf("independent expression finding lost: %s", &stdout)
				}
			})
		}
	}
}
