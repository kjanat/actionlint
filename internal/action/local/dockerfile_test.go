package local

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/fsread"
)

func TestDockerfileOnlyAction(t *testing.T) {
	for _, name := range []string{"Dockerfile", "dockerfile"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			file := writeFixture(t, root, "action/"+name, `FROM scratch
`)
			var inputs []string
			cache := NewCache(root, nil)
			cache.Configure(Options{Reader: fsread.New(t.Context(), nil, func(path string) { inputs = append(inputs, path) })})
			for _, wantCached := range []bool{false, true} {
				meta, cached, err := cache.FindMetadata("./action")
				if err != nil || meta != nil || cached != wantCached {
					t.Fatalf("metadata=%+v cached=%v error=%v", meta, cached, err)
				}
			}
			if len(inputs) != 1 || !strings.EqualFold(inputs[0], file) || len(cache.sources) != 0 {
				t.Fatalf("Dockerfile must be an input without manifest metadata: inputs=%v sources=%v", inputs, cache.sources)
			}
		})
	}
}

func TestDockerfileFallbackPrecedence(t *testing.T) {
	for _, names := range [][]string{
		{"action.yml", "action.yaml", "Dockerfile", "dockerfile"},
		{"action.yaml", "Dockerfile", "dockerfile"},
		{"action.yml", "Dockerfile", "dockerfile"},
		{"Dockerfile", "dockerfile"},
		{"dockerfile"},
	} {
		t.Run(strings.Join(names, "+"), func(t *testing.T) {
			root := t.TempDir()
			var inputs []string
			cache := NewCache(root, nil)
			cache.Configure(Options{CustomReader: true, Reader: fsread.New(t.Context(), func(path string) ([]byte, error) {
				name := filepath.Base(path)
				if !slices.Contains(names, name) {
					return nil, os.ErrNotExist
				}
				if strings.HasPrefix(name, "action.") {
					return []byte(`name: metadata
description: test
runs: {using: composite, steps: []}
`), nil
				}
				return []byte(`FROM scratch
`), nil
			}, func(path string) { inputs = append(inputs, filepath.Base(path)) })})
			meta, _, err := cache.FindMetadata("./action")
			if err != nil || len(inputs) != 1 || inputs[0] != names[0] || (meta != nil) != strings.HasPrefix(names[0], "action.") {
				t.Fatalf("metadata=%+v inputs=%v error=%v", meta, inputs, err)
			}
		})
	}
}

func TestDockerfileReadErrors(t *testing.T) {
	for _, readError := range []error{os.ErrPermission, errors.New("broken reader")} {
		cache := NewCache(t.TempDir(), nil)
		cache.Configure(Options{CustomReader: true, Reader: fsread.New(t.Context(), func(path string) ([]byte, error) {
			if filepath.Base(path) == "Dockerfile" {
				return nil, readError
			}
			return nil, os.ErrNotExist
		}, nil)})
		if _, _, err := cache.FindMetadata("./action"); !errors.Is(err, readError) {
			t.Fatalf("read error lost: %v", err)
		}
	}
}

func TestDockerfileDirectoryIsNotAction(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "action", "Dockerfile"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewCache(root, nil).FindMetadata("./action"); err == nil {
		t.Fatal("directory named Dockerfile accepted as an action")
	}
}

func TestMappedDockerfileCannotEscapeRepository(t *testing.T) {
	for _, name := range []string{"Dockerfile", "dockerfile"} {
		t.Run(name, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			file := writeFixture(t, outside, name, `FROM scratch
`)
			if err := os.Mkdir(filepath.Join(root, "action"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(file, filepath.Join(root, "action", name)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			view := NewResolver(NewCache(root, nil), map[string]string{"./generated": "./action"})
			if _, _, err := view.FindMetadata("./generated"); err == nil || !strings.Contains(err.Error(), "outside the repository") {
				t.Fatalf("mapped Dockerfile symlink accepted: %v", err)
			}
		})
	}
}

func TestMappedDockerfileInsideRepository(t *testing.T) {
	for _, name := range []string{"Dockerfile", "dockerfile"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			file := writeFixture(t, root, "action/"+name, `FROM scratch
`)
			var inputs []string
			cache := NewCache(root, nil)
			cache.Configure(Options{Reader: fsread.New(t.Context(), nil, func(path string) { inputs = append(inputs, path) })})
			view := NewResolver(cache, map[string]string{"./generated": "./action"})
			meta, _, err := view.FindMetadata("./generated")
			if err != nil || meta != nil || len(inputs) != 1 || !strings.EqualFold(inputs[0], file) {
				t.Fatalf("metadata=%+v inputs=%v error=%v", meta, inputs, err)
			}
			if len(view.Sources()) != 0 {
				t.Fatal("Dockerfile exposed as action metadata")
			}
		})
	}
}
