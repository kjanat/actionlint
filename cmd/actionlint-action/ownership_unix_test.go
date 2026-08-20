//go:build unix

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func ownerOf(t *testing.T, path string) (uint32, uint32) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("file ownership is unavailable")
	}
	return stat.Uid, stat.Gid
}

func TestWriteResultFileInheritsWorkspaceOwner(t *testing.T) {
	workspace := resolved(t, t.TempDir())
	target := filepath.Join("a", "b", "results.json")
	if _, err := writeResultFile(openRoot(t, workspace), target, "content"); err != nil {
		t.Fatal(err)
	}

	wantUID, wantGID := ownerOf(t, workspace)
	for _, path := range []string{filepath.Join(workspace, "a"), filepath.Join(workspace, "a", "b"), filepath.Join(workspace, target)} {
		uid, gid := ownerOf(t, path)
		if uid != wantUID || gid != wantGID {
			t.Errorf("%s: wanted owner %d:%d but got %d:%d", path, wantUID, wantGID, uid, gid)
		}
	}
}

func TestInheritOwnerReportsMissingPaths(t *testing.T) {
	workspace := resolved(t, t.TempDir())
	if err := inheritOwner(openRoot(t, workspace), []string{"missing"}); err == nil {
		t.Error("wanted an error for a missing path")
	}
}
