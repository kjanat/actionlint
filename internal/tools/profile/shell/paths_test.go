package shell

import "testing"

func TestRunnerPaths(t *testing.T) {
	repository, workspace, action := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)
	t.Setenv("GITHUB_ACTION_PATH", t.TempDir())
	if got := RunnerWorkspace(repository); got != workspace {
		t.Fatalf("runner workspace ignored: %q", got)
	}
	if got := RunnerActionPath(action); got != action {
		t.Fatalf("unrelated runner action path replaced the analyzed action: %q", got)
	}
	t.Setenv("GITHUB_WORKSPACE", "")
	t.Setenv("GITHUB_ACTION_PATH", action)
	if got := RunnerWorkspace(repository); got != repository {
		t.Fatalf("local fallback lost: %q", got)
	}
	if got := RunnerActionPath(action); got != action {
		t.Fatalf("matching runner action path lost: %q", got)
	}
	if got := RunnerActionPath(""); got != "" {
		t.Fatalf("unrelated ambient action context was accepted: %q", got)
	}
}
