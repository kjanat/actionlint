package workflownames

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestDarwinMissingAncestorCaseSensitivity(t *testing.T) {
	root := t.TempDir()
	value, err := syscall.Pathconf(root, 11)
	if err != nil || value < 0 {
		t.Skipf("filesystem case capability unavailable: %d, %v", value, err)
	}
	left, right := filepath.Join(root, ".github/workflows"), filepath.Join(root, ".GITHUB/WORKFLOWS")
	if got := SamePath(left, right); got != (value == 0) {
		t.Fatalf("case capability %d: path identity %t", value, got)
	}
}
