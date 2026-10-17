package application_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/tools/adapters"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestRepositoryActionDiscovery(t *testing.T) {
	for _, workflows := range []bool{false, true} {
		t.Run(map[bool]string{false: "actions only", true: "mixed"}[workflows], func(t *testing.T) {
			root := t.TempDir()
			writeGitRepository(t, root)
			want := []string{".github/actions/unused/action.yaml", "action.yml", "packages/build/action.yml"}
			for _, path := range want {
				writeFixture(t, root, path, compositeAction)
			}
			writeFixture(t, root, "generated/action.yml", "invalid: [\n")
			writeFixture(t, root, ".github/actionlint.yaml", "files: {excludes: ['generated/**']}\n")
			for _, directory := range []string{".git-cache/node_modules", "vendor", ".venv", "venv", ".cache", ".worktrees", "testdata", "internal/githubaction/tooling/testdata", "nested"} {
				writeFixture(t, root, directory+"/action.yml", "invalid: [\n")
			}
			writeFixture(t, root, "nested/.git", "gitdir: elsewhere\n")
			if workflows {
				writeFixture(t, root, ".github/workflows/action.yml", goodWorkflow)
				want = append(want, ".github/workflows/action.yml")
			}
			options := application.Options{WorkingDir: filepath.Join(root, "packages", "build")}
			result, err := application.Check(t.Context(), options, application.Input{Kind: application.Repository, Directory: ""})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 0 || len(result.Documents) != len(want) {
				t.Fatalf("want %d clean documents, got %+v", len(want), result)
			}
			for _, path := range want {
				if !slices.Contains(result.Inputs, filepath.Join(root, filepath.FromSlash(path))) {
					t.Errorf("missing discovered input %q in %v", path, result.Inputs)
				}
			}
		})
	}
}

func TestRepositoryActionDiscoverySymlinks(t *testing.T) {
	root := t.TempDir()
	writeGitRepository(t, root)
	manifest := writeFixture(t, root, "action.yml", compositeAction)
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(manifest, filepath.Join(root, "action.yaml")); err != nil {
		t.Fatal(err)
	}
	paths, err := workspace.RepositoryPaths(t.Context(), workspace.Identity(root), fsread.New(t.Context(), nil, nil))
	if err != nil || !slices.Equal(paths, []string{manifest}) {
		t.Fatalf("want only %q, got %v, error %v", manifest, paths, err)
	}
}

func TestRequiredToolsDiscoversActions(t *testing.T) {
	root := t.TempDir()
	writeGitRepository(t, root)
	writeFixture(t, root, "action.yml", compositeAction)
	writeFixture(t, root, ".github/actionlint.yaml", `lint:
  rules:
    external: {shellcheck: off, ruff: off}
overrides:
  - includes: [action.yml]
    lint:
      rules:
        external: {shellcheck: on, ruff: on}
`)
	options := application.Options{WorkingDir: root, Tools: (adapters.Options{ShellCheck: "shellcheck", Ruff: "ruff"}).Registrations()}
	got, err := application.RequiredTools(t.Context(), options, nil)
	if err != nil || !got["shellcheck"] || !got["ruff"] {
		t.Fatalf("action override must enable both tools; got %+v, error %v", got, err)
	}
}
