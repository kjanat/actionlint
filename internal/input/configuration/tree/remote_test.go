package tree

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRemoteExtendsFormatsAndOrigins(t *testing.T) {
	leaf := filepath.Join(t.TempDir(), "actionlint.yaml")
	remote := "https://example.test/config/base.toml?version=1"
	shared := "https://example.test/shared.json?version=2"
	contents := map[string]string{
		leaf:   "extends: [\"" + remote + "\", \"" + remote + "\"]\nleaf: true\n",
		remote: "extends = [\"../shared.json?version=2\"]\nbase = true\n",
		shared: `{"shared": true}`,
	}
	reads := map[string]int{}
	loaded, err := Load(leaf, func(name string) ([]byte, error) {
		reads[name]++
		content, ok := contents[name]
		if !ok {
			return nil, fmt.Errorf("unexpected source %q", name)
		}
		return []byte(content), nil
	}, nil)
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
	for i := 0; i < len(loaded.Node.Content); i += 2 {
		key, value := loaded.Node.Content[i], loaded.Node.Content[i+1]
		want := map[string]string{"shared": shared, "base": remote, "leaf": leaf, "extends": leaf}[key.Value]
		if loaded.Origins[value] != want {
			t.Fatalf("origin of %s = %q, want %q", key.Value, loaded.Origins[value], want)
		}
	}
}

func TestRemoteExtendsCycle(t *testing.T) {
	_, err := Load("https://example.test/a.yaml", func(name string) ([]byte, error) {
		if strings.HasSuffix(name, "/a.yaml") {
			return []byte("extends: [./b.yaml]"), nil
		}
		return []byte("extends: [./a.yaml]"), nil
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "cyclic config extends") {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestRemoteReaderReceivesDeclaredURL(t *testing.T) {
	leaf := filepath.Join(t.TempDir(), "actionlint.yaml")
	declared := "HTTPS://EXAMPLE.TEST/base.yaml?version=1"
	_, err := Load(leaf, func(name string) ([]byte, error) {
		switch name {
		case leaf:
			return []byte("extends: [\"" + declared + "\"]"), nil
		case declared:
			return []byte("base: true"), nil
		default:
			return nil, fmt.Errorf("source reader received rewritten URL %q", name)
		}
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRemoteDependencyCancellation(t *testing.T) {
	leaf := filepath.Join(t.TempDir(), "actionlint.yaml")
	_, err := Load(leaf, func(name string) ([]byte, error) {
		if name == leaf {
			return []byte("extends: [https://example.test/base.yaml]"), nil
		}
		return nil, context.Canceled
	}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("dependency lost cancellation: %v", err)
	}
}

func TestRemoteExtendsRejectsUnsafeAddressesBeforeReading(t *testing.T) {
	for _, reference := range []string{"http://example.test/a.yaml", "https://user:password@example.test/a.yaml", "file:///tmp/a.yaml", "https://example.test/a.yaml#fragment"} {
		t.Run(reference, func(t *testing.T) {
			reads := 0
			_, err := Load("https://example.test/base.yaml", func(string) ([]byte, error) {
				reads++
				return []byte("extends: [\"" + reference + "\"]"), nil
			}, nil)
			if err == nil || reads != 1 {
				t.Fatalf("error = %v, reads = %d", err, reads)
			}
		})
	}
}
