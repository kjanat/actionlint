package application

import "testing"

func TestGitModesShareOneSnapshotPerRoot(t *testing.T) {
	root, git := executableFixture(t)
	cache := &gitModes{}
	first := cache.load(t.Context(), root)
	if err := first.Err(); err != nil {
		t.Fatal(err)
	}
	writeShellcheckFixture(t, root, "later.sh", "echo later\n")
	git("add", "later.sh")
	again := cache.load(t.Context(), root)
	if again != first || again.Mode("later.sh") != "" {
		t.Fatal("snapshot changed after the repository changed within one analysis")
	}
	fresh := (&gitModes{}).load(t.Context(), root)
	if fresh.Err() != nil || fresh.Mode("later.sh") != "100644" {
		t.Fatalf("a new analysis did not read the updated index: mode=%q err=%v", fresh.Mode("later.sh"), fresh.Err())
	}
}
