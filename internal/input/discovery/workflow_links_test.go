package discovery

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/files"
)

func TestWorkflowDiscoveryFileLinks(t *testing.T) {
	t.Parallel()
	root, outside := t.TempDir(), t.TempDir()
	writeFixture(t, root, "regular.yml", "on: push\n")
	writeFixture(t, outside, "ci.yml", "on: push\n")
	for name, target := range map[string]string{"linked.yml": filepath.Join(outside, "ci.yml"), "action.yml": filepath.Join(outside, "ci.yml"), "linked-directory": outside} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	reader := files.New(t.Context(), nil, nil)
	workflows, err := Workflows(t.Context(), root, reader)
	want := []string{filepath.Join(root, "action.yml"), filepath.Join(root, "linked.yml"), filepath.Join(root, "regular.yml")}
	if err != nil || !slices.Equal(workflows, want) {
		t.Fatalf("workflow file links: %v, %v; want %v", workflows, err, want)
	}
	yaml, err := YAML(t.Context(), root, reader)
	if err != nil || !slices.Equal(yaml, []string{filepath.Join(root, "regular.yml")}) {
		t.Fatalf("ordinary directory followed links: %v, %v", yaml, err)
	}
	actions, err := Actions(t.Context(), root, reader)
	if err != nil || len(actions) != 0 {
		t.Fatalf("action discovery followed links: %v, %v", actions, err)
	}
}

func TestWorkflowDiscoverySkipsIgnoredFileLinks(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	gitFixture(t, root)
	writeFixture(t, root, ".gitignore", "ignored.yml\n")
	writeFixture(t, root, "ci.yml", "on: push\n")
	if err := os.Symlink("missing.yml", filepath.Join(root, "ignored.yml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	paths, err := Workflows(t.Context(), root, files.New(t.Context(), nil, nil))
	if err != nil || !slices.Equal(paths, []string{filepath.Join(root, "ci.yml")}) {
		t.Fatalf("ignored dangling file link affected discovery: %v, %v", paths, err)
	}
}
