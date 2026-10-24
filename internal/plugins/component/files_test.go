package component_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
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

func TestCallClosesRetainedHostFiles(t *testing.T) {
	closeFailure := errors.New("host file close failed")
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"success", nil},
		{"close failure", closeFailure},
		{"joined cancellation", errors.Join(context.Canceled, closeFailure)},
		{"joined deadline", errors.Join(context.DeadlineExceeded, closeFailure)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plugin := loadPlugin(t, "misbehave", component.Options{})
			for call := range 2 {
				files := &countedFiles{FS: fstest.MapFS{"source.sh": &fstest.MapFile{Data: []byte("echo hello")}}, closeErr: tc.err}
				_, err := plugin.Analyze(t.Context(), analyzer.Input{
					Content: "/workspace/source.sh", Options: json.RawMessage("retain-file"),
					Files: []analyzer.FileSystem{{Path: "/workspace", FS: preopens.ImmutableFS{FS: files}}},
				})
				if !errors.Is(err, tc.err) {
					t.Fatalf("call %d: got %v, want %v", call, err, tc.err)
				}
				openedSource := false
				for _, file := range files.opened {
					openedSource = openedSource || file.name == "source.sh"
					if file.closes != 1 {
						t.Errorf("call %d file %q closed %d times; want once", call, file.name, file.closes)
					}
				}
				if !openedSource {
					t.Fatalf("call %d never opened source.sh", call)
				}
			}
		})
	}
}

func TestCallPreservesHostCloseFailureAfterCancellation(t *testing.T) {
	plugin := loadPlugin(t, "misbehave", component.Options{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	closeErr := errors.New("cancelled call file close failed")
	files := &countedFiles{
		FS: fstest.MapFS{"source.sh": &fstest.MapFile{Data: []byte("echo hello")}}, closeErr: closeErr,
		onOpen: func(name string) {
			if name == "source.sh" {
				cancel()
			}
		},
	}
	_, err := plugin.Analyze(ctx, analyzer.Input{
		Content: "/workspace/source.sh", Options: json.RawMessage("retain-file"),
		Files: []analyzer.FileSystem{{Path: "/workspace", FS: preopens.ImmutableFS{FS: files}}},
	})
	if !errors.Is(err, context.Canceled) || !errors.Is(err, closeErr) {
		t.Fatalf("call lost cancellation or cleanup error: %v", err)
	}
	if len(files.opened) == 0 {
		t.Fatal("guest never opened a host file")
	}
	for _, file := range files.opened {
		if file.closes != 1 {
			t.Errorf("file %q closed %d times; want once", file.name, file.closes)
		}
	}
}

type countedFiles struct {
	fs.FS
	opened   []*countedFile
	closeErr error
	onOpen   func(string)
}

func (f *countedFiles) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	opened := &countedFile{File: file, name: name, closeErr: f.closeErr}
	f.opened = append(f.opened, opened)
	if f.onOpen != nil {
		f.onOpen(name)
	}
	return opened, nil
}

type countedFile struct {
	fs.File
	name     string
	closes   int
	closeErr error
}

func (f *countedFile) Close() error {
	f.closes++
	return errors.Join(f.File.Close(), f.closeErr)
}

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
