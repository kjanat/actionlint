package githubaction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestRuffDisableToolsInput(t *testing.T) {
	module, err := os.ReadFile(filepath.Join("..", "plugins", "core", "testdata", "engines", "echo.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	module, err = testutil.ReplaceWasmDataString(module, []byte(`"engine":{"name":"echo"`), []byte(`"engine":{"name":"ruff"`))
	if err != nil {
		t.Fatal(err)
	}
	modulePath := filepath.Join(t.TempDir(), "ruff.wasm")
	if err := os.WriteFile(modulePath, module, 0o600); err != nil {
		t.Fatal(err)
	}
	workspace := workspaceWith(t, map[string]string{".git": "", ".github/workflows/python.yml": `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(missing)
`, ".github/actionlint.yaml": "tools:\n  ruff:\n    backend:\n      wasm:\n        path: " + filepath.ToSlash(modulePath) + "\n"})
	for _, tc := range []struct {
		disabled, config string
		code             int
	}{{"shellcheck", "", 1}, {"shellcheck,ruff", "", 0}, {"shellcheck", "tools: {ruff: false}", 0}, {"shellcheck", "lint: {rules: {external: {ruff: off}}}", 0}} {
		env := map[string]string{"GITHUB_WORKSPACE": workspace, "INPUT_DISABLE-TOOLS": tc.disabled, "INPUT_CONFIG": tc.config}
		var out strings.Builder
		if code := Main(func(k string) string { return env[k] }, &out); code != tc.code {
			t.Fatalf("disable-tools=%q: exit=%d: %s", tc.disabled, code, &out)
		}
		if tc.code == 1 && !strings.Contains(out.String(), "F821") {
			t.Fatal(&out)
		}
		ruffDisabled := strings.Contains(tc.disabled, "ruff")
		if !ruffDisabled && !strings.Contains(out.String(), "(requested tools: ruff)") {
			t.Fatalf("missing requested Ruff status: %s", &out)
		}
		if ruffDisabled && !strings.Contains(out.String(), "(external linters disabled)") {
			t.Fatalf("incorrect disabled-tool status: %s", &out)
		}
	}
}
