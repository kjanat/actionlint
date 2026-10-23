package component_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/plugins/component"
	"github.com/kjanat/wacogo/wasi/filesystem/preopens"
)

func TestFileGrantsAreReadOnlyAndInvocationScoped(t *testing.T) {
	plugin := loadPlugin(t, "misbehave", component.Options{})
	for _, tc := range []struct {
		content, operation, want string
		grant                    bool
	}{
		{"first", "read-file", "file contents: first", true},
		{"", "read-file", "filesystem denied", false},
		{"second", "read-file", "file contents: second", true},
		{"original", "write-file", "write denied", true},
	} {
		input := analyzer.Input{Content: "/workspace/source.sh", Options: json.RawMessage(tc.operation)}
		if tc.grant {
			input.Files = []analyzer.FileSystem{{Path: "/workspace", FS: preopens.ImmutableFS{FS: fstest.MapFS{"source.sh": &fstest.MapFile{Data: []byte(tc.content)}}}}}
		}
		_, err := plugin.Analyze(t.Context(), input)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s grant=%v: %v", tc.operation, tc.grant, err)
		}
	}
}

func TestFileGrantPreservesSymlinkParentTraversal(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "real", "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"helper.sh": "wrong", "real/helper.sh": "correct"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join("real", "sub"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	var inputs []string
	files, err := fsread.NewSnapshot(t.Context(), root, fsread.New(t.Context(), nil, func(name string) { inputs = append(inputs, name) }), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	guest := loadPlugin(t, "misbehave", component.Options{})
	_, err = guest.Analyze(t.Context(), analyzer.Input{
		Content: "/workspace/link/../helper.sh", Options: json.RawMessage("read-file"),
		Files: []analyzer.FileSystem{{Path: "/workspace", FS: files}},
	})
	if err == nil || !strings.Contains(err.Error(), "file contents: correct") {
		t.Fatalf("symlink traversal changed: %v", err)
	}
	if !slices.Contains(inputs, filepath.Join(root, "real", "helper.sh")) || slices.Contains(inputs, filepath.Join(root, "helper.sh")) {
		t.Fatalf("wrong source identity: %v", inputs)
	}
}
