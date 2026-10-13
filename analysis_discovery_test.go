package actionlint

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestRepositoryActionDiscovery(t *testing.T) {
	for _, workflows := range []bool{false, true} {
		t.Run(map[bool]string{false: "actions only", true: "mixed"}[workflows], func(t *testing.T) {
			root := t.TempDir()
			writeShellcheckFixture(t, root, ".git/HEAD", "ref: refs/heads/main\n")
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
	writeShellcheckFixture(t, root, ".git/HEAD", "ref: refs/heads/main\n")
	manifest := writeShellcheckFixture(t, root, "action.yml", standaloneComposite)
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(manifest, filepath.Join(root, "action.yaml")); err != nil {
		t.Fatal(err)
	}
	paths, err := repositoryPaths(t.Context(), &Project{root: root}, nil)
	if err != nil || !slices.Equal(paths, []string{manifest}) {
		t.Fatalf("want only %q, got %v, error %v", manifest, paths, err)
	}
}

func TestRequiredToolsDiscoversActions(t *testing.T) {
	root := t.TempDir()
	writeShellcheckFixture(t, root, ".git/HEAD", "ref: refs/heads/main\n")
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

func TestRepositoryActionDiscoveryReportsMetadataInputs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git required")
	}
	root := t.TempDir()
	for _, args := range [][]string{{"init", "--quiet"}, {"config", "user.email", "fixture@example.invalid"}, {"config", "user.name", "Fixture"}} {
		if out, err := repositoryGit(t.Context(), root, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	writeShellcheckFixture(t, root, ".gitignore", "generated/\n")
	writeShellcheckFixture(t, root, "action.yml", standaloneComposite)
	writeShellcheckFixture(t, root, "generated/action.yml", "invalid: [\n")
	if out, err := repositoryGit(t.Context(), root, "add", "--", "action.yml").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Repository("")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 0 || len(result.Documents) != 1 {
		t.Fatalf("want one clean document, got %+v", result)
	}
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".gitignore", filepath.Join(".git", "config"), filepath.Join(".git", "index")} {
		if !slices.Contains(result.Inputs, filepath.Join(physical, name)) {
			t.Errorf("missing metadata input %q in %v", name, result.Inputs)
		}
	}
}
