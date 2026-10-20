package engine

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/repoconfig"
)

func TestShellcheckWorkflowPathValidation(t *testing.T) {
	command := shellcheckForTest(t)
	for _, selection := range []struct{ config, message string }{
		{`"${{ github.action_path }}/.shellcheckrc"`, "requires an analyzed composite action"},
		{`{source-path: ["${{ github.action_path }}/lib"]}`, "requires an analyzed composite action"},
		{`{source-path: ["${{ env.HOME }}"]}`, "unknown path interpolation"},
	} {
		t.Run(selection.config, func(t *testing.T) {
			for _, tc := range []struct{ name, step string }{
				{"uses only", "uses: actions/checkout@v6"},
				{"python", "shell: python\n        run: print('hello')"},
				{"powershell", "shell: pwsh\n        run: Write-Output hello"},
				{"bash", "shell: bash\n        run: echo hello"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					root := t.TempDir()
					config := writeShellcheckFixture(t, root, "actionlint.yaml", `tools:
  shellcheck:
    config: `+selection.config+"\n")
					workflow := writeShellcheckFixture(t, root, "workflow.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - `+tc.step+"\n")
					session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, ConfigFile: config, Shellcheck: command})
					if err != nil {
						t.Fatal(err)
					}
					_, err = session.Files([]string{workflow}, nil)
					if err == nil || !strings.Contains(err.Error(), selection.message) {
						t.Fatalf("ordinary workflow accepted invalid configuration: %v", err)
					}
				})
			}
		})
	}
}

func TestShellcheckRelativeProjectPaths(t *testing.T) {
	command := shellcheckForTest(t)
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("GITHUB_WORKSPACE", "")
	rc := writeShellcheckFixture(t, root, ".shellcheckrc", "disable=SC2086\n")
	writeShellcheckFixture(t, root, ".github/.shellcheckrc", "enable=all\n")
	content := []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo $VALUE
`)
	for _, selection := range []string{
		"${{ gitdir }}/.shellcheckrc",
		"${{ github.workspace }}/.shellcheckrc",
		"${{ configdir }}/../.shellcheckrc",
	} {
		t.Run(selection, func(t *testing.T) {
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: {config: '"+selection+"'}}\n")
			project, err := NewProject(".")
			if err != nil {
				t.Fatal(err)
			}
			result, err := Analyze(t.Context(), AnalysisRequest{
				ShellCheck: command,
				Sources:    []SourceUnit{{Path: ".github/workflows/test.yml", Content: content, Project: project, Config: project.Config()}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 0 || !slices.Contains(result.Inputs, rc) {
				t.Fatalf("relative project selected the wrong rc: diagnostics=%+v inputs=%v", result.Diagnostics, result.Inputs)
			}
		})
	}
}

func TestShellcheckPathContext(t *testing.T) {
	repository, workspace, action := t.TempDir(), t.TempDir(), t.TempDir()
	rule := newShellcheckRunForTest(&externalCommand{})
	rule.paths.workspace = repository
	rule.actionPath = action
	t.Setenv("GITHUB_WORKSPACE", workspace)
	t.Setenv("GITHUB_ACTION_PATH", t.TempDir())
	context := rule.pathContext()
	if context.GitDir != repository || context.Workspace != workspace || context.ActionPath != action {
		t.Fatalf("runner paths replaced the analyzed repository/action: %+v", context)
	}
	t.Setenv("GITHUB_WORKSPACE", "")
	t.Setenv("GITHUB_ACTION_PATH", action)
	context = rule.pathContext()
	if context.Workspace != repository || context.ActionPath != action {
		t.Fatalf("local fallback or matching runner context lost: %+v", context)
	}
	rule.actionPath = ""
	if got := rule.pathContext().ActionPath; got != "" {
		t.Fatalf("unrelated ambient action context was accepted: %q", got)
	}
}

func TestShellcheckApplicationPathOrigin(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	rc := writeShellcheckFixture(t, root, ".shellcheckrc", "disable=SC2086\n")
	projectRC := writeShellcheckFixture(t, root, ".github/.shellcheckrc", "disable=SC2046\n")
	configFile := writeShellcheckFixture(t, root, ".github/actionlint.yaml", "{}\n")
	for _, tc := range []struct{ selected, want string }{
		{".shellcheckrc", rc},
		{"${{ configdir }}/.shellcheckrc", projectRC},
	} {
		rule := newShellcheckRunForTest(&externalCommand{})
		loaded, err := repoconfig.Load(configFile)
		if err != nil {
			t.Fatal(err)
		}
		rule.cfg = loaded
		rule.config = &ShellcheckSettings{Config: ShellcheckRCFile(tc.selected)}
		if err := rule.prepareConfigPath(); err != nil {
			t.Fatal(err)
		}
		if got := rule.rcArgs[1]; got != tc.want {
			t.Errorf("%q: selected %q, want %q", tc.selected, got, tc.want)
		}
	}
}

func TestShellcheckInterpolatedPaths(t *testing.T) {
	command := shellcheckForTest(t)
	workspace := t.TempDir()
	repository := filepath.Join(workspace, "checkout")
	if err := os.MkdirAll(filepath.Join(repository, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_WORKSPACE", workspace)
	writeShellcheckFixture(t, workspace, "shared/env.sh", "VALUE=42\n")
	rc := writeShellcheckFixture(t, workspace, "rc/.shellcheckrc", "disable=SC2086\n")
	workflow := writeShellcheckFixture(t, repository, ".github/workflows/test.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: |
          . env.sh
          echo $VALUE
`)
	for _, selection := range []string{
		`"${{ github.workspace }}/rc/.shellcheckrc"`,
		`{source-path: ["${{ github.workspace }}/shared"]}`,
	} {
		config := writeShellcheckFixture(t, repository, ".github/actionlint.yaml", "tools: {shellcheck: {config: "+selection+"}}\n")
		session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: repository, ConfigFile: config, Shellcheck: command})
		if err != nil {
			t.Fatal(err)
		}
		result, err := session.Files([]string{workflow}, nil)
		if err != nil || len(result.Diagnostics) != 0 {
			t.Fatalf("interpolated config did not apply: %+v, %v", result, err)
		}
		if strings.Contains(selection, ".shellcheckrc") {
			found := false
			for _, input := range result.Inputs {
				found = found || input == rc
			}
			if !found {
				t.Fatalf("interpolated rc missing from inputs: %v", result.Inputs)
			}
		}
	}
}
