package workflow_test

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixturefiles"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/tempdir"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/results"
)

// duplicateWorkflowNameFindings returns "file:line:col: message" for each
// duplicate-workflow-name finding, sorted, with paths relative to root.
func duplicateWorkflowNameFindings(t *testing.T, root string, diagnostics []results.Diagnostic) []string {
	t.Helper()
	var got []string
	for _, d := range diagnostics {
		if d.Rule != "duplicate-workflow-name" {
			t.Fatalf("unexpected finding: %+v", d)
		}
		if d.Severity != "warning" {
			t.Fatalf("severity = %q, want warning: %+v", d.Severity, d)
		}
		rel, err := filepath.Rel(root, d.Path)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, filepath.ToSlash(rel)+":"+strconv.Itoa(d.Start.Line)+":"+strconv.Itoa(d.Start.Column)+": "+d.Message)
	}
	slices.Sort(got)
	return got
}

func analyzeWorkflowDirectory(t *testing.T, root, config string, paths ...string) []results.Diagnostic {
	t.Helper()
	cfg, err := checking.ConfigurationRules().ParseConfig([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	project := workspace.Identity(root)
	inputs := make([]workspace.InputFile, len(paths))
	for i, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		inputs[i] = workspace.InputFile{Path: path, Content: content, Project: project, Config: cfg}
	}
	result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: root}, inputs)
	if err != nil {
		t.Fatal(err)
	}
	return results.Diagnostics(result.Diagnostics)
}

// The shape of kjanat/shellcheck-rs at 646b1a6: two workflows are named
// "Prepare release" and three "Publish packages".
func TestDuplicateWorkflowNameRepository(t *testing.T) {
	t.Parallel()
	root := tempdir.Physical(t)
	var paths []string
	for _, file := range []struct{ path, name string }{
		{"prepare-go.yml", "Prepare release"},
		{"prepare-release.yml", "Prepare release"},
		{"publish-crates.yml", "Publish packages"},
		{"release-go.yml", "Publish packages"},
		{"release-rshellcheck.yml", "Publish packages"},
		{"release.yml", "Release rshellcheck"},
	} {
		paths = append(paths, fixturefiles.Write(t, root, ".github/workflows/"+file.path, "name: "+file.name+"\n"+testutil.GoodWorkflow))
	}
	got := duplicateWorkflowNameFindings(t, root, analyzeWorkflowDirectory(t, root, "", paths...))
	want := []string{
		`.github/workflows/prepare-go.yml:1:7: workflow name "Prepare release" is also used by .github/workflows/prepare-release.yml; the Actions tab cannot tell these workflows apart`,
		`.github/workflows/prepare-release.yml:1:7: workflow name "Prepare release" is also used by .github/workflows/prepare-go.yml; the Actions tab cannot tell these workflows apart`,
		`.github/workflows/publish-crates.yml:1:7: workflow name "Publish packages" is also used by .github/workflows/release-go.yml, .github/workflows/release-rshellcheck.yml; the Actions tab cannot tell these workflows apart`,
		`.github/workflows/release-go.yml:1:7: workflow name "Publish packages" is also used by .github/workflows/publish-crates.yml, .github/workflows/release-rshellcheck.yml; the Actions tab cannot tell these workflows apart`,
		`.github/workflows/release-rshellcheck.yml:1:7: workflow name "Publish packages" is also used by .github/workflows/publish-crates.yml, .github/workflows/release-go.yml; the Actions tab cannot tell these workflows apart`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// Only the checked file is reported; the others on disk supply the names.
	got = duplicateWorkflowNameFindings(t, root, analyzeWorkflowDirectory(t, root, "", paths[0]))
	if !slices.Equal(got, want[:1]) {
		t.Fatalf("single checked file: %v", got)
	}

	got = duplicateWorkflowNameFindings(t, root, analyzeWorkflowDirectory(t, root, "lint: {rules: {suspicious: {duplicate-workflow-name: off}}}", paths...))
	if len(got) != 0 {
		t.Fatalf("rule off still reported: %v", got)
	}
}

func TestDuplicateWorkflowNameCases(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc, checked, other string
		otherPath            string
		want                 string
	}{
		{desc: "case differs", checked: "CI", other: "ci", want: `workflow name "CI" is also used by .github/workflows/other.yml (as "ci"); the Actions tab cannot tell these workflows apart`},
		{desc: "distinct", checked: "CI", other: "CI lint"},
		{desc: "trailing space is not trimmed", checked: "CI", other: "'CI '"},
		{desc: "other unnamed", checked: ".github/workflows/other.yml"},
		{desc: "checked expression", checked: "${{ 'CI' }}", other: "CI"},
		{desc: "other expression", checked: "CI", other: "${{ 'CI' }}"},
		{desc: "dynamic other", checked: "CI", other: "CI ${{ github.ref }}"},
		{desc: "other not a direct workflow", checked: "CI", other: "CI", otherPath: ".github/workflows/nested/other.yml"},
		{desc: "other outside workflows", checked: "CI", other: "CI", otherPath: ".github/other.yml"},
		{desc: "other does not parse", checked: "CI", other: "CI\non: ["},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			root := tempdir.Physical(t)
			otherPath := tc.otherPath
			if otherPath == "" {
				otherPath = ".github/workflows/other.yml"
			}
			other := testutil.GoodWorkflow
			if tc.other != "" {
				other = "name: " + tc.other + "\n" + testutil.GoodWorkflow
			}
			fixturefiles.Write(t, root, otherPath, other)
			checked := fixturefiles.Write(t, root, ".github/workflows/checked.yml", "name: "+tc.checked+"\n"+testutil.GoodWorkflow)
			got := duplicateWorkflowNameFindings(t, root, analyzeWorkflowDirectory(t, root, "", checked))
			var want []string
			if tc.want != "" {
				want = []string{".github/workflows/checked.yml:1:7: " + tc.want}
			}
			if !slices.Equal(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}

// An editor buffer replaces the file on disk: renaming the open workflow
// removes the duplicate even before the file is saved, and a single known
// workflow never duplicates itself.
func TestDuplicateWorkflowNameInMemorySources(t *testing.T) {
	t.Parallel()
	root := tempdir.Physical(t)
	project := workspace.Identity(root)
	fixturefiles.Write(t, root, ".github/workflows/build.yml", "name: Build\n"+testutil.GoodWorkflow)
	open := fixturefiles.Write(t, root, ".github/workflows/open.yml", "name: Build\n"+testutil.GoodWorkflow)
	for _, tc := range []struct {
		content string
		want    int
	}{
		{"name: Build\n" + testutil.GoodWorkflow, 1},
		{"name: Renamed\n" + testutil.GoodWorkflow, 0},
	} {
		result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: root}, []workspace.InputFile{{Path: open, Content: []byte(tc.content), Project: project}})
		if err != nil {
			t.Fatal(err)
		}
		if len(duplicateWorkflowNameFindings(t, root, results.Diagnostics(result.Diagnostics))) != tc.want {
			t.Fatalf("content %q: %+v", tc.content, result.Diagnostics)
		}
	}

	alone := tempdir.Physical(t)
	path := filepath.Join(alone, ".github", "workflows", "only.yml")
	result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: alone}, []workspace.InputFile{{Path: path, Content: []byte("name: Build\n" + testutil.GoodWorkflow), Project: workspace.Identity(alone)}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("single in-memory workflow: %+v, %v", result.Diagnostics, err)
	}
}
