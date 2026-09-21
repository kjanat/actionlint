package actionlint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigPathInterpolation(t *testing.T) {
	root := t.TempDir()
	context := configPathContext{
		configDir: filepath.Join(root, "config with spaces"),
		gitDir:    filepath.Join(root, "repo"), workspace: root,
		actionPath: filepath.Join(root, "repo", ".github", "actions", "example"),
	}
	for _, tc := range []struct{ value, want string }{
		{"${{ configdir }}/.shellcheckrc", filepath.Join(context.configDir, ".shellcheckrc")},
		{"${{gitdir}}/scripts", filepath.Join(context.gitDir, "scripts")},
		{"${{ GITHUB.WORKSPACE }}/shared", filepath.Join(root, "shared")},
		{"${{ github.action_path }}/lib", filepath.Join(context.actionPath, "lib")},
		{"./relative", filepath.Join(context.configDir, "relative")},
	} {
		got, err := context.resolve(tc.value)
		if err != nil || got != tc.want {
			t.Errorf("%q: got %q, %v; want %q", tc.value, got, err, tc.want)
		}
	}
	for _, value := range []string{"${{ env.HOME }}", "${{ configdir", "${{ format('x') }}", "{configdir}/rc", "{gitdir}/rc"} {
		if _, err := context.resolve(value); err == nil {
			t.Errorf("accepted unsupported interpolation %q", value)
		}
	}
	context.actionPath = ""
	if _, err := context.expand("${{ github.action_path }}/lib"); err == nil {
		t.Error("invented action path outside an action context")
	}
	for _, literal := range []string{"literal-${{ env.HOME }}", "literal-{gitdir}"} {
		context.configDir = filepath.Join(root, literal)
		got, err := context.resolve("${{ configdir }}/lib")
		if err != nil || got != filepath.Join(context.configDir, "lib") {
			t.Fatalf("replacement value was interpreted recursively: %q, %v", got, err)
		}
	}
}

func TestShellcheckActionPathRequiresComposite(t *testing.T) {
	rule := newRuleShellcheck(&externalCommand{})
	rule.paths.workspace, rule.paths.analysis = t.TempDir(), t.TempDir()
	rule.config = &ShellcheckSettings{Config: ShellcheckRCFile("${{ github.action_path }}/.shellcheckrc")}
	if err := rule.VisitWorkflowPre(&Workflow{}); err != nil {
		t.Fatalf("resolved action context before visiting a composite: %v", err)
	}
	err := rule.VisitStep(&Step{Exec: &ExecRun{
		Run: &String{Value: "echo hello"}, Shell: &String{Value: "bash"}, RunPos: &Pos{Line: 1, Col: 1},
	}})
	if err == nil || !strings.Contains(err.Error(), "requires an analyzed composite action") {
		t.Fatalf("ordinary workflow run accepted an action-only path: %v", err)
	}
}

func TestShellcheckPathContext(t *testing.T) {
	repository, workspace, action := t.TempDir(), t.TempDir(), t.TempDir()
	rule := newRuleShellcheck(&externalCommand{})
	rule.paths.workspace = repository
	rule.actionPath = action
	t.Setenv("GITHUB_WORKSPACE", workspace)
	t.Setenv("GITHUB_ACTION_PATH", t.TempDir())
	context := rule.pathContext()
	if context.gitDir != repository || context.workspace != workspace || context.actionPath != action {
		t.Fatalf("runner paths replaced the analyzed repository/action: %+v", context)
	}
	t.Setenv("GITHUB_WORKSPACE", "")
	t.Setenv("GITHUB_ACTION_PATH", action)
	context = rule.pathContext()
	if context.workspace != repository || context.actionPath != action {
		t.Fatalf("local fallback or matching runner context lost: %+v", context)
	}
	rule.actionPath = ""
	if got := rule.pathContext().actionPath; got != "" {
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
		rule := newRuleShellcheck(&externalCommand{})
		rule.SetConfig(&Config{filename: configFile})
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
	workflow := writeShellcheckFixture(t, repository, ".github/workflows/test.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: |\n          . env.sh\n          echo $VALUE\n")
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
