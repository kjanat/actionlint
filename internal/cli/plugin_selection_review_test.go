package cli

import (
	"os"
	"path/filepath"
	"testing"

	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestPluginSelectionFailureExitStatus(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	for _, directory := range []string{".git", ".github"} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(".github", "actionlint.yaml"), []byte("plugins: ["), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"check", "lsp", "rules"} {
		pluginCommandTest(t, plugininstall.Acquisition{}, 3, command)
	}
}

func TestProjectPluginLockFlagIsRejected(t *testing.T) {
	t.Parallel()
	for _, command := range [][]string{{"check"}, {"lsp"}, {"rules"}, {"plugin", "install"}} {
		pluginCommandTest(t, plugininstall.Acquisition{}, 2, append(command, "--plugin-lock", "unused.json")...)
	}
}
