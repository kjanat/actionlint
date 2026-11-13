package process

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/integrations/process"
)

func TestNativeDirectoryBindsPhysicalToolPaths(t *testing.T) {
	t.Parallel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target, alias := filepath.Join(root, "physical"), filepath.Join(root, "alias")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	backend := &Backend{name: "fixture", command: &process.Command{Dir: alias}}
	for _, requested := range []string{"", alias} {
		actual, err := backend.directory(requested)
		if err != nil {
			t.Fatal(err)
		}
		if actual != target {
			t.Fatalf("tool paths bind to %q instead of execution directory %q", actual, target)
		}
	}
}
