package githubaction

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
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
