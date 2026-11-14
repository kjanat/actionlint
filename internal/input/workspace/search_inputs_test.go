package workspace_test

import (
	"path/filepath"
	"slices"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestRepositorySelectionIncludesActionsAndRespectsGitIgnore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeToolRepository(t, root)
	workflow := writeToolFixture(t, root, ".github/workflows/ci.yml", toolWorkflow)
	action := writeToolFixture(t, root, "action.yml", "name: local\nruns: {using: composite, steps: [{shell: bash, run: echo ok}]}\n")
	ignored := writeToolFixture(t, root, "generated/action.yml", "[invalid YAML\n")
	ignoreFile := writeToolFixture(t, root, ".gitignore", "generated/\n")
	for _, tc := range []struct {
		name     string
		noIgnore bool
	}{{"respect Git ignore", false}, {"include Git ignored files", true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			selected, err := workspace.FindRepositoryFiles(t.Context(), root, tc.noIgnore)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{workflow, action}
			if tc.noIgnore {
				want = append(want, ignored)
			}
			var got []string
			for _, file := range selected.Files {
				if file.Explicit {
					t.Errorf("discovered file %s was marked explicit", file.Path)
				}
				got = append(got, file.Path)
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("selected files=%v, want %v", got, want)
			}
			if !tc.noIgnore && !slices.Contains(selected.Inputs, ignoreFile) {
				t.Errorf("discovery did not record %s: %v", ignoreFile, selected.Inputs)
			}
		})
	}
}

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
		{[]string{".github/workflows"}, true, []string{"ci.yml", "excluded.yml", "ignored.yml"}},
		{[]string{".github/workflows/*.yml"}, true, []string{"ci.yml", "excluded.yml", "ignored.yml"}},
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

func TestNoIgnoreIncludesExcludedFilesWhenPlanningTools(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeToolRepository(t, root)
	writeToolFixture(t, root, ".github/workflows/ci.yml", toolWorkflow)
	writeToolFixture(t, root, ".github/actionlint.yaml", "files: {excludes: ['**']}\n")
	for _, noIgnore := range []bool{false, true} {
		for _, paths := range [][]string{nil, {".github/workflows"}, {".github/workflows/*.yml"}} {
			session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, NoIgnore: noIgnore, Rules: checking.ConfigurationRules()})
			if err != nil {
				t.Fatal(err)
			}
			needed, err := session.RequiredTools(t.Context(), paths, workspace.ToolOptions{
				Required: func(_, _ *repoconfig.Config) ([]string, error) { return []string{"fixture-tool"}, nil },
			})
			if err != nil || needed["fixture-tool"] != noIgnore {
				t.Fatalf("paths=%v no-ignore=%v: tools=%v, error=%v", paths, noIgnore, needed, err)
			}
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
