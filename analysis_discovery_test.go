package actionlint

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRepositoryActionDiscovery(t *testing.T) {
	for _, workflows := range []bool{false, true} {
		t.Run(map[bool]string{false: "actions only", true: "mixed"}[workflows], func(t *testing.T) {
			root := t.TempDir()
			writeShellcheckFixture(t, root, ".git", "gitdir: elsewhere\n")
			want := []string{".github/actions/unused/action.yaml", "action.yml", "packages/build/action.yml"}
			for _, path := range want {
				writeShellcheckFixture(t, root, path, standaloneComposite)
			}
			writeShellcheckFixture(t, root, "generated/action.yml", "invalid: [\n")
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "files: {excludes: ['generated/**']}\n")
			for _, directory := range []string{".git-cache/node_modules", "vendor", ".venv", "venv", ".cache", ".worktrees", "testdata", "scripts/testdata", "nested"} {
				writeShellcheckFixture(t, root, directory+"/action.yml", "invalid: [\n")
			}
			writeShellcheckFixture(t, root, "nested/.git", "gitdir: elsewhere\n")
			if workflows {
				writeShellcheckFixture(t, root, ".github/workflows/action.yml", commandGoodWorkflow)
				want = append(want, ".github/workflows/action.yml")
			}
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: filepath.Join(root, "packages", "build")})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Repository("")
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
	writeShellcheckFixture(t, root, ".git", "gitdir: elsewhere\n")
	manifest := writeShellcheckFixture(t, root, "action.yml", standaloneComposite)
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(manifest, filepath.Join(root, "action.yaml")); err != nil {
		t.Fatal(err)
	}
	paths, err := repositoryPaths(t.Context(), &Project{root: root})
	if err != nil || !slices.Equal(paths, []string{manifest}) {
		t.Fatalf("want only %q, got %v, error %v", manifest, paths, err)
	}
}

func TestRequiredToolsDiscoversActions(t *testing.T) {
	root := t.TempDir()
	writeShellcheckFixture(t, root, ".git", "gitdir: elsewhere\n")
	writeShellcheckFixture(t, root, "action.yml", standaloneComposite)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", `lint:
  rules:
    external: {shellcheck: off, ruff: off}
overrides:
  - includes: [action.yml]
    lint:
      rules:
        external: {shellcheck: on, ruff: on}
`)
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, Shellcheck: "shellcheck", Ruff: "ruff"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := session.RequiredTools(nil)
	if err != nil || !got.Shellcheck || !got.Ruff {
		t.Fatalf("action override must enable both tools; got %+v, error %v", got, err)
	}
}
