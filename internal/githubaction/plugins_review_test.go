package githubaction

import (
	"fmt"
	"path/filepath"
	"testing"

	actionlint "actionlint.kjanat.dev/internal/engine"
)

func TestActionRunsConfiguredWasmWithoutNativeProvisioning(t *testing.T) {
	module, err := filepath.Abs(filepath.Join("..", "corewasm", "testdata", "engines", "echo.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("tools:\n  shellcheck: false\n  ruff:\n    backend:\n      wasm: {path: %q}\n", filepath.ToSlash(module))
	root := workspaceWith(t, map[string]string{
		".git": "", ".github/actionlint.yaml": config,
		".github/workflows/ci.yml": "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\n",
	})
	req := &lintRequest{ctx: t.Context(), workingDir: root, workspaceDir: root, format: formatJSON, ruff: "ruff", ruffOptions: &actionlint.ExternalCommandOptions{Optional: true}}
	result := runLinter(req)
	if result.code != actionlint.ExitStatusSuccessProblemFound || len(result.diagnostics) != 1 || result.diagnostics[0].Code != "F821" {
		t.Fatalf("result=%+v diagnostics=%+v", result.lintOutcome, result.diagnostics)
	}
	env := map[string]string{"GITHUB_WORKSPACE": root, "INPUT_RUFF": "true"}
	needed, err := requiredTools(func(key string) string { return env[key] })
	if err != nil || needed.Ruff || needed.Shellcheck {
		t.Fatalf("needed=%+v err=%v", needed, err)
	}
}
