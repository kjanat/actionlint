package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"golang.org/x/sys/execabs"
)

func TestExecutableLookupSharesConcurrentSearches(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(path, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	var lookup executableLookup
	calls := 0
	find := func(string) (string, error) { calls++; return path, nil }
	var group sync.WaitGroup
	for range 10 {
		group.Go(func() {
			got, err := lookup.resolve(find)
			if err != nil || got != path {
				t.Errorf("path=%q error=%v", got, err)
			}
		})
	}
	group.Wait()
	if calls != 1 {
		t.Fatalf("searched %d times, want one shared search", calls)
	}
}

func TestExecutableLookupRetriesFailuresAndRemovedFiles(t *testing.T) {
	t.Parallel()
	var lookup executableLookup
	calls := 0
	missing := func(string) (string, error) { calls++; return "", exec.ErrNotFound }
	for range 2 {
		if _, err := lookup.resolve(missing); !errors.Is(err, exec.ErrNotFound) {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(path, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := lookup.resolve(func(string) (string, error) { calls++; return path, nil }); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := lookup.resolve(missing); !errors.Is(err, exec.ErrNotFound) {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatalf("searched %d times, want failed searches and deleted executable to be retried", calls)
	}
}

func TestExecutableLookupFollowsSearchEnvironment(t *testing.T) {
	for _, variable := range []string{"PATH", "PATHEXT", "GODEBUG", "NoDefaultCurrentDirectoryInExePath"} {
		t.Run(variable, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "git")
			if err := os.WriteFile(path, nil, 0o700); err != nil {
				t.Fatal(err)
			}
			var lookup executableLookup
			calls := 0
			find := func(string) (string, error) { calls++; return path, nil }
			if _, err := lookup.resolve(find); err != nil {
				t.Fatal(err)
			}
			if variable == "NoDefaultCurrentDirectoryInExePath" {
				_, present := os.LookupEnv(variable)
				t.Setenv(variable, "")
				if present {
					if err := os.Unsetenv(variable); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				t.Setenv(variable, os.Getenv(variable)+"-changed")
			}
			if _, err := lookup.resolve(find); err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatalf("%s change did not trigger a search", variable)
			}
		})
	}
}

func TestExecutableLookupPreservesPathOrderAndRejectsLocalGit(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	name := "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for _, directory := range []string{first, second} {
		if err := os.WriteFile(filepath.Join(directory, name), nil, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var lookup executableLookup
	t.Setenv("PATH", first+string(os.PathListSeparator)+second)
	got, err := lookup.resolve(execabs.LookPath)
	if err != nil || got != filepath.Join(first, name) {
		t.Fatalf("first lookup: %q %v", got, err)
	}
	t.Setenv("PATH", second+string(os.PathListSeparator)+first)
	got, err = lookup.resolve(execabs.LookPath)
	if err != nil || got != filepath.Join(second, name) {
		t.Fatalf("changed PATH: %q %v", got, err)
	}
	t.Chdir(first)
	t.Setenv("PATH", "."+string(os.PathListSeparator)+second)
	t.Setenv("NoDefaultCurrentDirectoryInExePath", "1")
	if got, err := lookup.resolve(execabs.LookPath); err == nil || got != "" {
		t.Fatalf("local executable accepted: %q %v", got, err)
	}
}
