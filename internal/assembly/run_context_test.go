package assembly

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/configuration/configreport"
	"actionlint.kjanat.dev/v2/internal/integrations/gitcommand"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func runContextRepository(t *testing.T) (string, func(...string)) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git required")
	}
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		args = append([]string{"-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "-c", "core.autocrlf=false"}, args...)
		if out, err := gitcommand.Command(t.Context(), gitPath, root, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "--quiet")
	return root, git
}

func writeRunContextFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryStatesSeparateCleanAndDirtyLintedFiles(t *testing.T) {
	t.Parallel()
	root, git := runContextRepository(t)
	ci := filepath.Join(root, ".github", "workflows", "ci.yml")
	release := filepath.Join(root, ".github", "workflows", "release.yml")
	writeRunContextFile(t, ci, "on: push\n")
	writeRunContextFile(t, release, "on: push\n")
	git("add", ".")
	git("commit", "--quiet", "--no-verify", "-m", "initial")
	linted := []report.LintedFile{{Path: ci}, {Path: release}}

	clean := repositoryStates(t.Context(), root, linted)
	if len(clean) != 1 || clean[0].Kind != ReadRepository || clean[0].Root != root {
		t.Fatalf("clean states = %+v, want one read repository at %s", clean, root)
	}
	if clean[0].Worktree.Dirty() || len(clean[0].ChangedFiles) != 0 || len(clean[0].Worktree.Head) != 40 {
		t.Fatalf("clean state = %+v, want a committed HEAD and no changes", clean[0])
	}

	writeRunContextFile(t, ci, "on: pull_request\n")
	writeRunContextFile(t, filepath.Join(root, "notes.txt"), "unrelated\n")
	dirty := repositoryStates(t.Context(), root, linted)
	if len(dirty) != 1 || !dirty[0].Worktree.Dirty() {
		t.Fatalf("dirty states = %+v, want a dirty repository", dirty)
	}
	if want := []string{filepath.Join(".github", "workflows", "ci.yml")}; !slices.Equal(dirty[0].ChangedFiles, want) {
		t.Fatalf("ChangedFiles = %q, want only the changed linted file %q", dirty[0].ChangedFiles, want)
	}
	if want := []string{".github/workflows/ci.yml", "notes.txt"}; !slices.Equal(dirty[0].Worktree.Changes, want) {
		t.Fatalf("Changes = %q, want %q", dirty[0].Worktree.Changes, want)
	}
}

func TestRepositoryStatesReportFilesOutsideRepositories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yml")
	writeRunContextFile(t, path, "on: push\n")
	states := repositoryStates(t.Context(), dir, []report.LintedFile{{Path: path}, {Path: "<stdin>"}})
	if len(states) != 1 || states[0].Kind != NoRepository {
		// A temporary directory inside a checkout would belong to that repository.
		if len(states) == 1 && states[0].Kind == ReadRepository {
			t.Skipf("temporary directory is inside repository %s", states[0].Root)
		}
		t.Fatalf("states = %+v, want one entry without a repository", states)
	}
}

func TestNewRunContextAccountsRulesAndIgnores(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	config := filepath.Join(dir, "actionlint.yaml")
	writeRunContextFile(t, config, "lint: {}\n")
	result := &report.Analysis{
		CollectedByRule: []report.RuleCollection{
			{Rule: "unpinned-reference", Findings: 2},
			{Rule: "expression", Findings: 0},
			{Rule: "unpinned-reference", Findings: 1},
			{Rule: "shellcheck", Findings: 1},
		},
		Diagnostics: []results.PublishedDiagnostic{{Rule: "shellcheck"}},
		Configurations: []configreport.Report{{
			Project: dir, File: config,
			Inspection: configreport.Inspection{
				Config: map[string]any{
					"ignores": []any{map[string]any{"rule": "shellcheck", "file": "ci.yml"}},
					"lint":    map[string]any{"rules": map[string]any{"security": map[string]any{"unpinned-reference": map[string]any{"level": "off"}}}},
					"output":  map[string]any{"color": "never"},
				},
				Origins: map[string]configreport.Origin{
					"/ignores": {Source: "config", State: "value", File: config, Line: 3},
					"/lint":    {Source: "config", State: "value", File: config, Line: 1},
					"/lint/rules/security/unpinned-reference/level": {Source: "config", State: "value", File: config, Line: 5},
					"/lint/rules/security/script-injection/level":   {Source: "default", State: "missing"},
					"/output/color": {Source: "config", State: "value", File: config, Line: 8},
				},
			},
		}},
		InlineSuppressions: []report.InlineSuppression{
			{Path: filepath.Join(dir, "ci.yml"), Line: 9, Rule: "script-injection"},
			{Path: filepath.Join(dir, "ci.yml"), Line: 9, Rule: "script-injection"},
		},
	}
	run := NewRunContext(t.Context(), RunContextInputs{
		Options:         checking.Options{WorkingDir: dir, IgnorePatterns: []string{"^SC2086"}},
		IgnoreSelectors: []string{"unpinned-reference"},
		Getenv:          func(string) string { return "secret" },
		Result:          result,
	})
	if run.RulesRun != 3 {
		t.Errorf("RulesRun = %d, want 3", run.RulesRun)
	}
	wantRules := []RuleOutcome{{Rule: "shellcheck", Collected: 1, Reported: 1}, {Rule: "unpinned-reference", Collected: 3, Reported: 0}}
	if !slices.Equal(run.Rules, wantRules) {
		t.Errorf("Rules = %+v, want %+v", run.Rules, wantRules)
	}
	wantIgnores := []IgnoreEntry{
		{Source: IgnoreSelectorFlag, Value: "unpinned-reference"},
		{Source: IgnoreRegexFlag, Value: "^SC2086"},
		{Source: IgnoreConfig, Value: "rule=shellcheck file=ci.yml", Location: "actionlint.yaml:3"},
		{Source: IgnoreInline, Value: "script-injection", Location: "ci.yml:9"},
	}
	if !slices.Equal(run.Ignores, wantIgnores) {
		t.Errorf("Ignores = %+v, want %+v", run.Ignores, wantIgnores)
	}
	if len(run.Configurations) != 1 {
		t.Fatalf("Configurations = %+v", run.Configurations)
	}
	// Output settings and defaults are left out; containers such as /lint are not values.
	wantSettings := []ChangedSetting{
		{Pointer: "/ignores", Value: `[{"file":"ci.yml","rule":"shellcheck"}]`, Origin: "actionlint.yaml:3"},
		{Pointer: "/lint/rules/security/unpinned-reference/level", Value: `"off"`, Origin: "actionlint.yaml:5"},
	}
	if got := run.Configurations[0].Settings; !slices.Equal(got, wantSettings) {
		t.Errorf("Settings = %+v, want %+v", got, wantSettings)
	}
	if files := run.Configurations[0].Files; len(files) != 1 || files[0].Path != "actionlint.yaml" || len(files[0].Digest) != 64 {
		t.Errorf("config Files = %+v, want actionlint.yaml with its digest", files)
	}
	if run.Network.GitHubToken != "GH_TOKEN" {
		t.Errorf("GitHubToken = %q, want the variable name GH_TOKEN", run.Network.GitHubToken)
	}
}

func TestSettingValueFollowsEscapedPointers(t *testing.T) {
	t.Parallel()
	config := map[string]any{"local-actions": map[string]any{"./a/b": "x"}, "list": []any{"first", "second"}}
	for pointer, want := range map[string]any{"/local-actions/.~1a~1b": "x", "/list/1": "second"} {
		if got, found := settingValue(config, pointer); !found || got != want {
			t.Errorf("settingValue(%q) = %v, %t; want %v", pointer, got, found, want)
		}
	}
	for _, pointer := range []string{"/missing", "/list/2", "/list/x", "/local-actions/.~1a~1b/deeper"} {
		if _, found := settingValue(config, pointer); found {
			t.Errorf("settingValue(%q) found a value", pointer)
		}
	}
}
