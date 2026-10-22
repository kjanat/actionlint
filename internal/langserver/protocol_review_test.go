package langserver

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestUNCFileURI(t *testing.T) {
	if runtime.GOOS != "windows" {
		if _, err := filePath("file://server/share/workflow.yml"); err == nil {
			t.Fatal("accepted a remote file authority on a non-Windows host")
		}
		return
	}
	for _, tc := range []struct{ uri, path string }{
		{"file://server/share/workflow.yml", `\\server\share\workflow.yml`},
		{"file://server/share/a%20b/%23workflow.yml", `\\server\share\a b\#workflow.yml`},
	} {
		t.Run(tc.uri, func(t *testing.T) {
			got, err := filePath(tc.uri)
			if err != nil || got != tc.path {
				t.Fatalf("filePath(%q)=%q, %v; want %q", tc.uri, got, err, tc.path)
			}
			if got := FileURI(tc.path); got != tc.uri {
				t.Fatalf("FileURI(%q)=%q; want %q", tc.path, got, tc.uri)
			}
		})
	}
}

func TestFileURILocalPathsAndInvalidAuthorities(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workflow.yml")
	uri := FileURI(path)
	if got, err := filePath(uri); err != nil || got != path {
		t.Fatalf("local path round trip=%q, %v; want %q", got, err, path)
	}
	for _, uri := range []string{
		"file://user@server/share/workflow.yml",
		"file://server:123/share/workflow.yml",
		"file://server/share/workflow.yml?query",
		"file://server/share/workflow.yml#fragment",
	} {
		if path, err := filePath(uri); err == nil {
			t.Fatalf("accepted unsupported file URI %q as %q", uri, path)
		}
	}
}
