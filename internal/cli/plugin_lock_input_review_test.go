package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestCheckPreservesInactivePluginLockInputs(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "stale"}[stale], func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			t.Setenv("ACTIONLINT_CONFIG", "")
			t.Setenv("ACTIONLINT_NO_CONFIG", "")
			if err := os.Mkdir(".git", 0o700); err != nil {
				t.Fatal(err)
			}
			lockPath := filepath.Join(root, ".github", pluginregistry.LockName)
			lock, err := pluginregistry.OpenLock(t.Context(), lockPath)
			if err != nil {
				t.Fatal(err)
			}
			if stale {
				registry := testutil.NewPluginRegistry(t)
				registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
				client, err := pluginregistry.New(testutil.FixtureRegistryURL)
				if err != nil {
					t.Fatal(err)
				}
				pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest")
				if err != nil {
					t.Fatal(err)
				}
				if err := lock.Set(pin); err != nil {
					t.Fatal(err)
				}
			}
			if err := lock.Save(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".github", "actionlint.yaml"), []byte("plugins: []\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile("workflow.yml", []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			output := pluginCommandTest(t, 3, "check", "--no-tool=ruff", "--no-tool=shellcheck", "--yes", "--output-file", lockPath, "workflow.yml")
			if !strings.Contains(output, "also an input") {
				t.Fatalf("missing collision diagnostic: %s", output)
			}
			after, err := os.ReadFile(lockPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("inactive lock overwritten: %s, %v", after, err)
			}
		})
	}
}
