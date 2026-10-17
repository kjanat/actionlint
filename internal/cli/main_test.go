package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/internal/testutil"
)

func TestMain(m *testing.M) {
	if os.Getenv("ACTIONLINT_TEST_RUFF_DISCOVERY") == "1" {
		os.Exit(testutil.RuffDiscoveryHelper())
	}
	if err := os.Chdir("../.."); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dir, err := isolateGitConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// Tests that create real repositories must not see the developer's Git configuration.
func isolateGitConfig() (string, error) {
	dir, err := os.MkdirTemp("", "actionlint-git-config-")
	if err != nil {
		return "", err
	}
	global := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(global, nil, 0o600); err != nil {
		return "", err
	}
	for name, value := range map[string]string{"GIT_CONFIG_GLOBAL": global, "GIT_CONFIG_NOSYSTEM": "1", "XDG_CONFIG_HOME": dir} {
		if err := os.Setenv(name, value); err != nil {
			return "", err
		}
	}
	return dir, nil
}
