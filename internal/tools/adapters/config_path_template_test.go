package adapters

import (
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
)

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
