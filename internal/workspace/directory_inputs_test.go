package workspace_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestDirectoryInputsDiscoverScopedDocuments(t *testing.T) {
	root := t.TempDir()
	writeToolRepository(t, root)
	writeToolFixture(t, root, ".github/actionlint.yaml", "files: {excludes: ['.github/workflows/excluded.yml']}\n")
	writeToolFixture(t, root, ".github/dependabot.yml", "version: 2\n")
	writeToolFixture(t, root, ".github/workflows/ci.yml", toolWorkflow)
	writeToolFixture(t, root, ".github/workflows/nested/more.yaml", toolWorkflow)
	writeToolFixture(t, root, ".github/workflows/excluded.yml", "[not read")
	writeToolFixture(t, root, ".github/actions/build/action.yml", "name: build\n")
	writeToolFixture(t, root, "custom/action.yaml", "name: custom\n")
	writeToolFixture(t, root, "compose.yml", "services: {}\n")
	for _, tc := range []struct {
		path string
		want []string
	}{
		{".", []string{".github/actions/build/action.yml", ".github/workflows/ci.yml", ".github/workflows/nested/more.yaml", "custom/action.yaml"}},
		{".github", []string{".github/actions/build/action.yml", ".github/workflows/ci.yml", ".github/workflows/nested/more.yaml"}},
		{".github/workflows", []string{".github/workflows/ci.yml", ".github/workflows/nested/more.yaml"}},
		{".github/actions", []string{".github/actions/build/action.yml"}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root})
			if err != nil {
				t.Fatal(err)
			}
			selection, err := session.Files([]string{tc.path}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, source := range selection.Sources {
				got = append(got, filepath.ToSlash(source.Path))
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("selected %v, want %v", got, tc.want)
			}
			if !slices.Contains(selection.Inputs, filepath.Join(root, filepath.FromSlash(tc.path))) {
				t.Fatalf("listed directory missing from inputs: %v", selection.Inputs)
			}
		})
	}
}

func TestDirectoryInputsDeduplicateDiscoveryAndKeepExplicitDuplicates(t *testing.T) {
	root := t.TempDir()
	writeToolFixture(t, root, "workflows/a.yml", toolWorkflow)
	writeToolFixture(t, root, "workflows/b.yaml", toolWorkflow)
	var selected []string
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, OnFilesSelected: func(paths []string) {
		selected = slices.Clone(paths)
	}})
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"workflows", "workflows/.", "workflows/a.yml", "workflows/a.yml"}
	selection, err := session.Files(paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Sources) != 3 || len(selected) != 3 || !slices.Equal(paths, []string{"workflows", "workflows/.", "workflows/a.yml", "workflows/a.yml"}) {
		t.Fatalf("sources=%d callback=%v arguments=%v", len(selection.Sources), selected, paths)
	}
	if filepath.Base(selection.Sources[0].Path) != "b.yaml" || selection.Sources[1].Path != selection.Sources[2].Path {
		t.Fatalf("directory and explicit input order changed: %+v", selection.Sources)
	}
}

func TestDirectoryInputsSkipDependenciesAndNestedRepositories(t *testing.T) {
	root := t.TempDir()
	writeToolFixture(t, root, "ci.yml", toolWorkflow)
	writeToolFixture(t, root, "nested/action.yml", "name: local\n")
	for _, directory := range []string{"node_modules", "vendor", "testdata", ".cache", "other"} {
		writeToolFixture(t, root, directory+"/nested/ignored.yaml", "[not read")
	}
	if err := os.Mkdir(filepath.Join(root, "other", ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: filepath.Dir(root), SkipProjectConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := session.Files([]string{root}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Sources) != 2 {
		t.Fatalf("dependency or nested repository selected: %+v", selection.Sources)
	}
}

func TestDirectoryInputsReportEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Files([]string{"."}, nil); err == nil || !strings.Contains(err.Error(), "no workflow or action files") {
		t.Fatalf("empty directory error: %v", err)
	}
}

func TestDirectoryInputsDiscoverGithubWithoutGitRepository(t *testing.T) {
	root := t.TempDir()
	writeToolFixture(t, root, ".github/workflows/ci.yml", toolWorkflow)
	writeToolFixture(t, root, ".github/actionlint.yml", "[not a workflow")
	writeToolFixture(t, root, ".github/dependabot.yml", "version: 2\n")
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, SkipProjectConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := session.Files([]string{"."}, nil)
	if err != nil || len(selection.Sources) != 1 || filepath.Base(selection.Sources[0].Path) != "ci.yml" {
		t.Fatalf("standalone .github directory: %+v, %v", selection, err)
	}
}

func TestDirectoryInputsSkipDiscoveredWorkflowDirectoryLinks(t *testing.T) {
	for _, link := range []string{".github", ".github/workflows"} {
		t.Run(link, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			writeToolFixture(t, root, "action.yml", "name: local\n")
			writeToolFixture(t, outside, "ci.yml", toolWorkflow)
			writeToolFixture(t, outside, "workflows/ci.yml", toolWorkflow)
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, filepath.FromSlash(link))), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(root, filepath.FromSlash(link))); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, SkipProjectConfig: true})
			if err != nil {
				t.Fatal(err)
			}
			selection, err := session.Files([]string{"."}, nil)
			if err != nil || len(selection.Sources) != 1 || filepath.Base(selection.Sources[0].Path) != "action.yml" {
				t.Fatalf("followed discovered directory link: %+v, %v", selection, err)
			}
		})
	}
}

func TestDirectoryInputsHonourGitIgnoreForYaml(t *testing.T) {
	root := t.TempDir()
	writeToolRepository(t, root)
	writeToolFixture(t, root, ".gitignore", "workflows/ignored.yml\nworkflows/generated/\n")
	writeToolFixture(t, root, "workflows/ci.yml", toolWorkflow)
	writeToolFixture(t, root, "workflows/ignored.yml", "[not read")
	writeToolFixture(t, root, "workflows/generated/ignored.yaml", "[not read")
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := session.Files([]string{"workflows"}, nil)
	if err != nil || len(selection.Sources) != 1 || filepath.Base(selection.Sources[0].Path) != "ci.yml" {
		t.Fatalf("ignored YAML was discovered: %+v, %v", selection, err)
	}
}
