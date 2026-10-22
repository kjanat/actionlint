package cli

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/internal/pluginregistry"
)

func TestPluginSelectionFailureExitStatus(t *testing.T) {
	for _, scenario := range []string{"config", "lock", "missing-lock", "flags"} {
		t.Run(scenario, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("ACTIONLINT_CONFIG", "")
			t.Setenv("ACTIONLINT_NO_CONFIG", "")
			for _, dir := range []string{".git", ".github"} {
				if err := os.Mkdir(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"check", "--no-ruff", "--no-shellcheck", "workflow.yml"}
			want := 3
			switch scenario {
			case "config":
				if err := os.WriteFile(filepath.Join(".github", "actionlint.yaml"), []byte("plugins: ["), 0o600); err != nil {
					t.Fatal(err)
				}
			case "lock":
				if err := os.WriteFile(filepath.Join(".github", pluginregistry.LockName), []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "missing-lock":
				args = append(args, "--plugin-lock", "missing.lock")
			case "flags":
				args = append(args, "--plugin", "missing.wasm", "--plugin-lock", "missing.lock")
				want = 2
			}
			pluginCommandTest(t, want, args...)
			args[0] = "lsp"
			args = append(args[:3], args[4:]...)
			pluginCommandTest(t, want, args...)
			args = append([]string{"rules"}, args[3:]...)
			pluginCommandTest(t, want, args...)
		})
	}
}
