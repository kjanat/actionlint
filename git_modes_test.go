package actionlint

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGitIndexLocationCompatibility(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git compatibility shim requires a Unix shell")
	}
	root, _ := executableFixture(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	shimDir := t.TempDir()
	const shim = `#!/bin/sh
case " $* " in
  *" rev-parse "*)
    if [ "$ACTIONLINT_TEST_INDEX_MODE" = old ]; then
      for arg do
        if [ "$arg" = --path-format=absolute ]; then
          printf '%s\n' "$arg"
        fi
      done
      exec "$ACTIONLINT_TEST_REAL_GIT" -C "$ACTIONLINT_TEST_REPOSITORY" rev-parse --git-path index
    fi
    printf '%s' "$ACTIONLINT_TEST_INDEX_OUTPUT"
    exit 0
    ;;
esac
exec "$ACTIONLINT_TEST_REAL_GIT" "$@"
`
	if err := os.WriteFile(filepath.Join(shimDir, "git"), []byte(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_TEST_REAL_GIT", realGit)
	t.Setenv("ACTIONLINT_TEST_REPOSITORY", root)
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, tc := range []struct {
		name, mode, output string
		invalid            bool
	}{
		{"pre-2.31 option echo", "old", "", false},
		{"relative", "output", ".git/index\n", false},
		{"absolute", "output", filepath.Join(root, ".git", "index") + "\r\n", false},
		{"empty", "output", "\n", true},
		{"multiple lines", "output", "--path-format=absolute\n.git/index\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ACTIONLINT_TEST_INDEX_MODE", tc.mode)
			t.Setenv("ACTIONLINT_TEST_INDEX_OUTPUT", tc.output)
			snapshot := (&gitModes{}).load(t.Context(), root)
			if tc.invalid {
				if snapshot.err == nil || snapshot.index != "" {
					t.Fatalf("invalid index location accepted: %q, %v", snapshot.index, snapshot.err)
				}
				return
			}
			want := filepath.Join(root, ".git", "index")
			if snapshot.err != nil || snapshot.index != want || snapshot.modes["bad.sh"] != "100644" {
				t.Fatalf("index location = %q, %v; want %q with parsed modes", snapshot.index, snapshot.err, want)
			}
		})
	}
}

func TestRepositoryGitDisablesFSMonitor(t *testing.T) {
	root, configure := executableFixture(t)
	configure("config", "core.fsmonitor", "true")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	output, err := repositoryGit(t.Context(), git, root, "config", "--type=bool", "--get", "core.fsmonitor").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "false" {
		t.Fatalf("index queries must disable repository fsmonitor: %q, %v", output, err)
	}
	snapshot := (&gitModes{}).load(t.Context(), root)
	if snapshot.err != nil || snapshot.modes["bad.sh"] != "100644" {
		t.Fatalf("index mode query failed: %v, %v", snapshot.modes, snapshot.err)
	}
}

func TestGitIndexSeparateDirectory(t *testing.T) {
	root := t.TempDir()
	metadata := filepath.Join(t.TempDir(), "metadata")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git required")
	}
	if output, err := repositoryGit(t.Context(), git, root, "init", "--quiet", "--separate-git-dir", metadata).CombinedOutput(); err != nil {
		t.Fatalf("initialize repository: %v: %s", err, output)
	}
	writeShellcheckFixture(t, root, "file.sh", "echo hello\n")
	if output, err := repositoryGit(t.Context(), git, root, "add", "file.sh").CombinedOutput(); err != nil {
		t.Fatalf("index file: %v: %s", err, output)
	}
	snapshot := (&gitModes{}).load(t.Context(), root)
	if snapshot.err != nil {
		t.Fatal(snapshot.err)
	}
	actual, err := os.Stat(snapshot.index)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(metadata, "index")
	expected, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(actual, expected) {
		t.Fatalf("separate index location %q does not identify %q", snapshot.index, want)
	}
}
