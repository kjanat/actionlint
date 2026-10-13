package projectpath

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func gitFixture(t *testing.T, root, name, content string) string {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return full
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestActionsHonoursGitIgnoreRules checks that discovery inside a Git work tree
// only yields manifests Git would not ignore, while the conventional directory
// list still applies. Regression for an ignored, vendored copy of another
// repository being linted (kjanat/actionlint#274).
func TestActionsHonoursGitIgnoreRules(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	tracked := gitFixture(t, root, "action.yml", "name: tracked\n")
	untracked := gitFixture(t, root, "packages/build/action.yaml", "name: untracked\n")
	// An ignored directory, one hidden through .git/info/exclude and a fixture
	// directory: none of these manifests may be discovered.
	gitFixture(t, root, ".gitignore", "ignored-docs/\n")
	gitFixture(t, root, "ignored-docs/action.yml", "name: ignored\n")
	gitFixture(t, root, ".git/info/exclude", "hidden/\n")
	gitFixture(t, root, "hidden/action.yml", "name: hidden\n")
	gitFixture(t, root, "testdata/action.yml", "name: fixture\n")
	runGit(t, root, "add", "action.yml")

	got, err := Actions(root)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	want := []string{tracked, untracked}
	if !slices.Equal(got, want) {
		t.Fatalf("Actions = %v, want %v", got, want)
	}
}
