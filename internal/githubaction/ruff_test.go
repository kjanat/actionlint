package githubaction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestRuffActionInput(t *testing.T) {
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
		input, config string
		code          int
	}{{"true", "", 1}, {"false", "", 0}, {"true", "tools: {ruff: false}", 0}, {"true", "lint: {rules: {external: {ruff: off}}}", 0}, {"invalid", "", 2}} {
		env := map[string]string{"GITHUB_WORKSPACE": workspace, "INPUT_SHELLCHECK": "false", "INPUT_RUFF": tc.input, "INPUT_CONFIG": tc.config}
		var out strings.Builder
		if code := Main(func(k string) string { return env[k] }, &out); code != tc.code {
			t.Fatalf("input=%q: exit=%d: %s", tc.input, code, &out)
		}
		if tc.code == 1 && !strings.Contains(out.String(), "F821") {
			t.Fatal(&out)
		}
		if tc.input == "true" && !strings.Contains(out.String(), "(requested tools: ruff)") {
			t.Fatalf("missing requested Ruff status: %s", &out)
		}
		if tc.input == "false" && !strings.Contains(out.String(), "(external linters disabled)") {
			t.Fatalf("incorrect disabled-tool status: %s", &out)
		}
	}
}
