package plugin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// CI supplies the custom binary after make lint-build.
func TestContractReload(t *testing.T) {
	binary := os.Getenv("ACTIONLINT_CUSTOM_GCL")
	if binary == "" {
		t.Skip("set ACTIONLINT_CUSTOM_GCL to the custom golangci-lint binary")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name, content string) {
		filename := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module actionlint.kjanat.dev/v2\n\ngo 1.27.0\n")
	write("a/a.go", "package a\nimport _ \"actionlint.kjanat.dev/v2/b\"\n")
	write("b/b.go", "package b\n")
	write(".golangci.yml", `version: "2"
linters:
  default: none
  enable: [actionlintarch]
  settings:
    custom:
      actionlintarch:
        type: module
        settings:
          contract: architecture.json
`)
	run := func(wantExit int, wantText string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), binary, "run", "./...")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOLANGCI_LINT_CACHE="+filepath.Join(dir, "cache"), "GOWORK=off")
		output, err := cmd.CombinedOutput()
		exit := 0
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				exit = e.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		if exit != wantExit || !strings.Contains(string(output), wantText) {
			t.Fatalf("exit %d, want %d containing %q:\n%s", exit, wantExit, wantText, output)
		}
	}
	write("architecture.json", `{"module":"actionlint.kjanat.dev/v2","layers":[{"name":"core","roots":["a","b"],"uses":[]}]}`)
	run(0, "0 issues")
	// Only the contract changes. Neither source files nor linter settings change.
	write("architecture.json", `{"module":"actionlint.kjanat.dev/v2","layers":[{"name":"core","roots":["a"],"uses":[]},{"name":"outer","roots":["b"],"uses":["core"]}]}`)
	run(1, "ALA101")
	write("architecture.json", `{"module":"actionlint.kjanat.dev/v2","layers":[{"name":"core","roots":["a"],"uses":[]}]}`)
	run(1, "ALA100")
	if err := os.Remove(filepath.Join(dir, "architecture.json")); err != nil {
		t.Fatal(err)
	}
	run(3, "architecture contract")
}
