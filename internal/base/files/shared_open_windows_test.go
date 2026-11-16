//go:build windows

package files

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestSharedReaderAcceptsOpenRenameHandle(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "artifact")
	if err := os.WriteFile(path, []byte("published artifact"), 0600); err != nil {
		t.Fatal(err)
	}
	name, err := sharedFilePath(path)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := windows.CreateFile(name, windows.GENERIC_READ|windows.DELETE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := windows.CloseHandle(writer); err != nil {
			t.Error(err)
		}
	})
	reader, err := OpenShared(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	actual, err := io.ReadAll(reader)
	if err != nil || string(actual) != "published artifact" {
		t.Fatalf("shared read returned %q with error %v", actual, err)
	}
}
