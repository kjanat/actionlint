package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
)

const pythonWorkflow = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\n"

func writeEngineConfig(t *testing.T, module string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "actionlint.yaml")
	if err := os.WriteFile(path, []byte("tools:\n  ruff:\n    backend:\n      wasm: {path: "+module+"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The repository backend applies to the default Ruff selection, so these runs omit --ruff.
func runWithDefaultRuff(input string, args ...string) commandTranscript {
	var out, errout bytes.Buffer
	cmd := cli.Command{Stdin: strings.NewReader(input), Stdout: &out, Stderr: &errout}
	status := cmd.Main(append([]string{"actionlint", "--shellcheck=", "--no-color", "check", "--summary=false"}, args...))
	return commandTranscript{status, out.String(), errout.String()}
}

func TestCheckRunsConfiguredWasmEngine(t *testing.T) {
	config := writeEngineConfig(t, filepath.Join(componentTestdata, "..", "..", "core", "testdata", "engines", "echo.wasm"))
	got := runWithDefaultRuff(pythonWorkflow, "--config", config, "--log-level", "debug", "-")
	if got.Status != 1 || !strings.Contains(got.Stdout, "<stdin>:6:20: Ruff reported issue in this script: F821: Undefined name `missing` [ruff]") {
		t.Fatalf("status=%d stdout=%q stderr=%q", got.Status, got.Stdout, got.Stderr)
	}
	if !strings.Contains(got.Stderr, `[Linter] Loaded engine "actionlint-engine-echo" (echo 1.0.0) from `) {
		t.Fatalf("debug log: %q", got.Stderr)
	}
	disabled := testRunCommand(pythonWorkflow, "check", "--config", config, "-")
	if disabled.Status != 0 || disabled.Stdout != "" {
		t.Fatalf("disabled: %+v", disabled)
	}
}

func TestCheckReportsEngineLoadFailures(t *testing.T) {
	config := writeEngineConfig(t, filepath.Join(componentTestdata, "wat", "forbidden-import.wasm"))
	got := runWithDefaultRuff(pythonWorkflow, "--config", config, "-")
	if got.Status != 3 || !strings.Contains(got.Stderr, "could not initialize Ruff: plugin ") ||
		!strings.Contains(got.Stderr, "forbidden-import.wasm") ||
		!strings.Contains(got.Stderr, "plugin imports wasi:sockets/tcp-create-socket@0.2.9, which this host does not grant") {
		t.Fatalf("status=%d stdout=%q stderr=%q", got.Status, got.Stdout, got.Stderr)
	}
}
