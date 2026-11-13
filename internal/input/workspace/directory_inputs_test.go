package workspace_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestDirectoryInputsDiscoverScopedDocuments(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
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
			t.Parallel()
			session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, Rules: checking.ConfigurationRules()})
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

func TestDirectoryInputsDeduplicateDiscoveryAndExplicitFiles(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeToolFixture(t, root, ".github/workflows/a.yml", toolWorkflow)
	writeToolFixture(t, root, ".github/workflows/b.yaml", toolWorkflow)
	var selected []string
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, OnFilesSelected: func(paths []string) {
		selected = slices.Clone(paths)
	}, Rules: checking.ConfigurationRules()})
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{".github/workflows", ".github/workflows/.", ".github/workflows/a.yml", ".github/workflows/a.yml"}
	selection, err := session.Files(paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Sources) != 2 || len(selected) != 2 || !slices.Equal(paths, []string{".github/workflows", ".github/workflows/.", ".github/workflows/a.yml", ".github/workflows/a.yml"}) {
		t.Fatalf("sources=%d callback=%v arguments=%v", len(selection.Sources), selected, paths)
	}
	if filepath.Base(selection.Sources[0].Path) != "b.yaml" || filepath.Base(selection.Sources[1].Path) != "a.yml" {
		t.Fatalf("directory and explicit input order changed: %+v", selection.Sources)
	}
}

func TestDirectoryInputsKeepDirectoryNamesAndEnterNestedRepositories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeToolFixture(t, root, ".github/workflows/ci.yml", toolWorkflow)
	writeToolFixture(t, root, "nested/action.yml", "name: local\n")
	for _, directory := range []string{"node_modules", "vendor", "testdata", ".cache"} {
		writeToolFixture(t, root, directory+"/.github/workflows/ci.yaml", toolWorkflow)
	}
	writeToolRepository(t, filepath.Join(root, "other"))
	writeToolFixture(t, root, "other/.github/workflows/ci.yaml", toolWorkflow)
	writeToolFixture(t, root, "other/arbitrary.yaml", "[not read")
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: filepath.Dir(root), SkipProjectConfig: true, Rules: checking.ConfigurationRules()})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := session.Files([]string{root}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Sources) != 7 {
		t.Fatalf("directory name excluded or nested repository missed: %+v", selection.Sources)
	}
}

func TestDirectoryInputsReportEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, Rules: checking.ConfigurationRules()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Files([]string{"."}, nil); err == nil || !strings.Contains(err.Error(), "no workflow or action files") {
		t.Fatalf("empty directory error: %v", err)
	}
}

func TestDirectoryInputsDiscoverGithubWithoutGitRepository(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeToolFixture(t, root, ".github/workflows/ci.yml", toolWorkflow)
	writeToolFixture(t, root, ".github/actionlint.yml", "[not a workflow")
	writeToolFixture(t, root, ".github/dependabot.yml", "version: 2\n")
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, SkipProjectConfig: true, Rules: checking.ConfigurationRules()})
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
			t.Chdir(root)
			writeToolFixture(t, root, "action.yml", "name: local\n")
			writeToolFixture(t, outside, "ci.yml", toolWorkflow)
			writeToolFixture(t, outside, "workflows/ci.yml", toolWorkflow)
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, filepath.FromSlash(link))), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(root, filepath.FromSlash(link))); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, SkipProjectConfig: true, Rules: checking.ConfigurationRules()})
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

func TestDirectoryInputsDiscoverWorkflowFileLinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	t.Chdir(root)
	writeToolFixture(t, root, ".github/workflows/regular.yml", toolWorkflow)
	writeToolFixture(t, outside, "linked.yml", toolWorkflow)
	link := filepath.Join(root, ".github", "workflows", "linked.yml")
	if err := os.Symlink(filepath.Join(outside, "linked.yml"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, path := range []string{".", ".github", ".github/workflows"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, SkipProjectConfig: true, Rules: checking.ConfigurationRules()})
			if err != nil {
				t.Fatal(err)
			}
			selection, err := session.Files([]string{path}, nil)
			if err != nil || len(selection.Sources) != 2 || !slices.Contains(selection.Inputs, link) {
				t.Fatalf("workflow file link missing: %+v, %v", selection, err)
			}
		})
	}
}

func TestDirectoryInputsHonourGitIgnoreForYaml(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeToolRepository(t, root)
	writeToolFixture(t, root, ".gitignore", ".github/workflows/ignored.yml\n.github/workflows/generated/\n")
	writeToolFixture(t, root, ".github/workflows/ci.yml", toolWorkflow)
	writeToolFixture(t, root, ".github/workflows/ignored.yml", "[not read")
	writeToolFixture(t, root, ".github/workflows/generated/ignored.yaml", "[not read")
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, Rules: checking.ConfigurationRules()})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := session.Files([]string{".github/workflows"}, nil)
	if err != nil || len(selection.Sources) != 1 || filepath.Base(selection.Sources[0].Path) != "ci.yml" {
		t.Fatalf("ignored YAML was discovered: %+v, %v", selection, err)
	}
}
