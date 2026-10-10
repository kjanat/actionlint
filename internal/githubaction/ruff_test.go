package githubaction

import (
	"os/exec"
	"strings"
	"testing"
)

func TestRuffActionInput(t *testing.T) {
	command, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("Ruff is not installed")
	}
	workspace := workspaceWith(t, map[string]string{".git": "", ".github/workflows/python.yml": "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print(missing)\n"})
	for _, tc := range []struct {
		input, config string
		code          int
	}{{"true", "", 1}, {"false", "", 0}, {"true", "tools: {ruff: false}", 0}, {"true", "lint: {rules: {external: {ruff: off}}}", 0}, {"invalid", "", 2}} {
		env := map[string]string{"GITHUB_WORKSPACE": workspace, "INPUT_SHELLCHECK": "false", "INPUT_RUFF": tc.input, "ACTIONLINT_RUFF_COMMAND": command, "INPUT_CONFIG": tc.config}
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
