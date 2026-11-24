package document

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRemoteExtendsFormatsAndOrigins(t *testing.T) {
	t.Parallel()
	leaf := filepath.Join(t.TempDir(), "actionlint.yaml")
	remote := "https://example.test/config/base.toml?version=1"
	shared := "https://example.test/shared.json?version=2"
	contents := map[string]string{
		leaf:   "extends: [\"" + remote + "\", \"" + remote + "\"]\nleaf: true\n",
		remote: "extends = [\"../shared.json?version=2\"]\nbase = true\n",
		shared: `{"shared": true}`,
	}
	reads := map[string]int{}
	loaded, err := LoadConfigDocument(leaf, nil, func(name string) ([]byte, error) {
		reads[name]++
		content, ok := contents[name]
		if !ok {
			return nil, fmt.Errorf("unexpected source %q", name)
		}
		return []byte(content), nil
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(loaded.Files, []string{shared, remote, leaf}) {
		t.Fatalf("sources = %v", loaded.Files)
	}
	for name, count := range reads {
		if count != 1 {
			t.Fatalf("read %s %d times", name, count)
		}
	}
	for key, want := range map[string]string{"shared": shared, "base": remote, "leaf": leaf} {
		if got := loaded.Root.Origins()["/"+key].File; got != want {
			t.Fatalf("origin of %s = %q, want %q", key, got, want)
		}
	}
}

func TestRemoteExtendsCycle(t *testing.T) {
	t.Parallel()
	_, err := LoadConfigDocument("https://example.test/a.yaml", nil, func(name string) ([]byte, error) {
		if strings.HasSuffix(name, "/a.yaml") {
			return []byte("extends: [./b.yaml]"), nil
		}
		return []byte("extends: [./a.yaml]"), nil
	}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "cyclic config extends") {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestRemoteReaderReceivesDeclaredURL(t *testing.T) {
	t.Parallel()
	leaf := filepath.Join(t.TempDir(), "actionlint.yaml")
	declared := "HTTPS://EXAMPLE.TEST/base.yaml?version=1"
	_, err := LoadConfigDocument(leaf, nil, func(name string) ([]byte, error) {
		switch name {
		case leaf:
			return []byte("extends: [\"" + declared + "\"]"), nil
		case declared:
			return []byte("base: true"), nil
		default:
			return nil, fmt.Errorf("source reader received rewritten URL %q", name)
		}
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRemoteDependencyCancellation(t *testing.T) {
	t.Parallel()
	leaf := filepath.Join(t.TempDir(), "actionlint.yaml")
	_, err := LoadConfigDocument(leaf, nil, func(name string) ([]byte, error) {
		if name == leaf {
			return []byte("extends: [https://example.test/base.yaml]"), nil
		}
		return nil, context.Canceled
	}, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("dependency lost cancellation: %v", err)
	}
}

func TestRemoteExtendsRejectsUnsafeAddressesBeforeReading(t *testing.T) {
	t.Parallel()
	for _, reference := range []string{"http://example.test/a.yaml", "https://user:password@example.test/a.yaml", "file:///tmp/a.yaml", "https://example.test/a.yaml#fragment"} {
		t.Run(reference, func(t *testing.T) {
			t.Parallel()
			reads := 0
			_, err := LoadConfigDocument("https://example.test/base.yaml", nil, func(string) ([]byte, error) {
				reads++
				return []byte("extends: [\"" + reference + "\"]"), nil
			}, nil, nil)
			if err == nil || reads != 1 {
				t.Fatalf("error = %v, reads = %d", err, reads)
			}
		})
	}
}

func TestInheritedNullThenPartialMappingOrigin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yaml")
	middle := filepath.Join(dir, "middle.yaml")
	leaf := filepath.Join(dir, "leaf.yaml")
	files := map[string]string{base: "tools: null\n", middle: "extends: [./base.yaml]\ntools:\n  shellcheck: false\n", leaf: "extends: [./middle.yaml]\ntools:\n  pyflakes: false\n"}
	loaded, err := LoadConfigDocument(leaf, nil, func(path string) ([]byte, error) {
		data, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("unknown path %q", path)
		}
		return []byte(data), nil
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := loaded.Root.Origins()["/tools"]
	if source.File != leaf || source.Reset == nil || source.Reset.File != base || source.Reset.Value.Line != 1 {
		t.Fatalf("lost inherited reset: %+v", source)
	}
	if loaded.Root.Origins()["/tools/shellcheck"].File != middle || loaded.Root.Origins()["/tools/pyflakes"].File != leaf {
		t.Fatalf("wrong child origins: %+v", loaded.Root.Origins())
	}
}

func TestExtendsPreservesCauseWithoutExposingMissingBase(t *testing.T) {
	t.Parallel()
	leaf := filepath.Join(t.TempDir(), "leaf.yaml")
	marker := &testSourceError{}
	for _, tc := range []struct {
		name                    string
		cause                   error
		wantMarker, wantMissing bool
	}{
		{"source error", marker, true, false},
		{"missing base", fmt.Errorf("missing: %w", os.ErrNotExist), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := LoadConfigDocument(leaf, nil, func(path string) ([]byte, error) {
				if path == leaf {
					return []byte("extends: [./base.yaml]"), nil
				}
				return nil, tc.cause
			}, nil, nil)
			var got *testSourceError
			if errors.As(err, &got) != tc.wantMarker || errors.Is(err, os.ErrNotExist) != tc.wantMissing {
				t.Fatalf("lost cause boundary: %v", err)
			}
		})
	}
}

type testSourceError struct{}

func (*testSourceError) Error() string { return "source error" }

func TestLoadUsesHeldSourcesWithoutReading(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yaml")
	leaf := filepath.Join(dir, "leaf.yaml")
	sources := map[string]string{base: "base: true\n", leaf: "extends: [./base.yaml]\nleaf: true\n"}
	files := NewFiles()
	first, err := LoadConfigDocument(leaf, files, func(path string) ([]byte, error) {
		data, ok := sources[path]
		if !ok {
			return nil, fmt.Errorf("unknown path %q", path)
		}
		return []byte(data), nil
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	spelled := dir + string(filepath.Separator) + "." + string(filepath.Separator) + "leaf.yaml"
	seen := map[string]string{}
	second, err := LoadConfigDocument(spelled, files, func(path string) ([]byte, error) {
		t.Errorf("read held source %q again", path)
		return nil, os.ErrNotExist
	}, func(origin string, source *Node) (*Node, error) {
		for pointer, location := range source.Origins() {
			if pointer != "" {
				seen[origin] = location.File
			}
		}
		return source, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(second.Root.Value()) != fmt.Sprint(first.Root.Value()) || !slices.Equal(second.Files, first.Files) {
		t.Fatalf("held load = %v %v, want %v %v", second.Root.Value(), second.Files, first.Root.Value(), first.Files)
	}
	if seen[leaf] != spelled || seen[base] != base {
		t.Fatalf("normalizer locations = %v", seen)
	}
	if string(second.Contents[spelled]) != sources[leaf] || string(second.Contents[base]) != sources[base] {
		t.Fatalf("contents = %q", second.Contents)
	}
	if second.Root.Origins()["/leaf"].File != leaf {
		t.Fatalf("merged origin = %+v", second.Root.Origins()["/leaf"])
	}
}
