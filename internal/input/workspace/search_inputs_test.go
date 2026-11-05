package workspace_test

import (
	"path/filepath"
	"slices"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestSearchAcrossRepositoriesUsesLocalIgnoresAndConfiguration(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)
	for _, name := range []string{"one", "two"} {
		root := filepath.Join(parent, name)
		writeToolRepository(t, root)
		writeToolFixture(t, root, ".github/workflows/ci.yml", toolWorkflow)
		writeToolFixture(t, root, "compose.yml", "services: {}\n")
		writeToolFixture(t, root, "nested/arbitrary.yml", "not: a workflow\n")
		writeToolFixture(t, root, "generated/action.yml", "[must not be read")
	}
	one, two := filepath.Join(parent, "one"), filepath.Join(parent, "two")
	writeToolFixture(t, one, ".gitignore", "generated/\n")
	writeToolFixture(t, two, ".github/actionlint.yaml", "files: {excludes: ['generated/**']}\n")
	writeToolFixture(t, two, ".gitignore", ".github/workflows/hidden.yml\n")
	writeToolFixture(t, two, ".github/workflows/hidden.yml", "[must not be read")
	for _, inputs := range [][]string{{parent}, {filepath.Join(parent, "*")}, {one, two, parent}} {
		session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: parent, Rules: checking.ConfigurationRules()})
		if err != nil {
			t.Fatal(err)
		}
		selection, err := session.Files(inputs, nil)
		if err != nil {
			t.Fatal(err)
		}
		var paths []string
		for _, source := range selection.Sources {
			paths = append(paths, source.InputPath)
		}
		slices.Sort(paths)
		want := []string{filepath.Join(one, ".github", "workflows", "ci.yml"), filepath.Join(two, ".github", "workflows", "ci.yml")}
		if !slices.Equal(paths, want) {
			t.Fatalf("inputs=%v selected=%v, want=%v", inputs, paths, want)
		}
	}
}

func TestSearchGlobsAreDiscoveryAndLiteralFilesOverrideFilters(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeToolRepository(t, root)
	writeToolFixture(t, root, ".gitignore", ".github/workflows/ignored.yml\n")
	writeToolFixture(t, root, ".github/actionlint.yaml", "files: {excludes: ['.github/workflows/excluded.yml']}\n")
	for _, name := range []string{"ci", "ignored", "excluded"} {
		writeToolFixture(t, root, ".github/workflows/"+name+".yml", toolWorkflow)
	}
	for _, tc := range []struct {
		inputs   []string
		noIgnore bool
		want     []string
	}{
		{[]string{".github/workflows"}, false, []string{"ci.yml"}},
		{[]string{".github/workflows/*.yml"}, false, []string{"ci.yml"}},
		{[]string{".github/workflows"}, true, []string{"ci.yml", "ignored.yml"}},
		{[]string{".github/workflows/excluded.yml", ".github/workflows/ignored.yml"}, false, []string{"excluded.yml", "ignored.yml"}},
		{[]string{".github/workflows", ".github/workflows/excluded.yml", ".github/workflows/excluded.yml"}, false, []string{"ci.yml", "excluded.yml"}},
	} {
		session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, NoIgnore: tc.noIgnore, Rules: checking.ConfigurationRules()})
		if err != nil {
			t.Fatal(err)
		}
		selection, err := session.Files(tc.inputs, nil)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, source := range selection.Sources {
			got = append(got, filepath.Base(source.Path))
		}
		slices.Sort(got)
		if !slices.Equal(got, tc.want) {
			t.Fatalf("inputs=%v no-ignore=%v: got=%v want=%v", tc.inputs, tc.noIgnore, got, tc.want)
		}
	}
}

func TestSearchDoesNotEnterGitIgnoredNestedRepository(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeToolRepository(t, root)
	writeToolFixture(t, root, ".gitignore", "ignored/\n")
	writeToolFixture(t, root, ".github/workflows/ci.yml", toolWorkflow)
	nested := filepath.Join(root, "ignored")
	writeToolRepository(t, nested)
	writeToolFixture(t, nested, ".github/workflows/ci.yml", toolWorkflow)
	for _, noIgnore := range []bool{false, true} {
		selection, err := workspace.FindFiles(t.Context(), []string{root}, noIgnore)
		want := 1
		if noIgnore {
			want = 2
		}
		if err != nil || len(selection.Files) != want {
			t.Fatalf("no-ignore=%v: files=%v error=%v", noIgnore, selection.Files, err)
		}
	}
}

func TestSearchExpandsHomeAndReportsUnmatchedGlob(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	writeToolFixture(t, root, "projects/one/.github/workflows/ci.yml", toolWorkflow)
	selection, err := workspace.FindFiles(t.Context(), []string{"~/projects/*"}, false)
	if err != nil || len(selection.Files) != 1 || selection.Files[0].Explicit {
		t.Fatalf("home glob: %+v, %v", selection, err)
	}
	if _, err := workspace.FindFiles(t.Context(), []string{filepath.Join(root, "missing-*")}, false); err == nil {
		t.Fatal("unmatched glob reported an empty successful scan")
	}
}
