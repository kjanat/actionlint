package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/integrations/gitcommand"
)

func writeToolFixture(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const toolWorkflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"

func writeToolRepository(t *testing.T, root string) {
	t.Helper()
	command := gitcommand.Command(t.Context(), "git", ".", "init", "--quiet", root)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
}
