package actionlint

import (
	"os/exec"
	"strings"
	"testing"
)

func TestRepositoryGitDisablesFSMonitor(t *testing.T) {
	root, configure := executableFixture(t)
	configure("config", "core.fsmonitor", "true")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	output, err := repositoryGit(t.Context(), git, root, "config", "--type=bool", "--get", "core.fsmonitor").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "false" {
		t.Fatalf("index queries must disable repository fsmonitor: %q, %v", output, err)
	}
	snapshot := (&gitModes{}).load(t.Context(), root)
	if snapshot.err != nil || snapshot.modes["bad.sh"] != "100644" {
		t.Fatalf("index mode query failed: %v, %v", snapshot.modes, snapshot.err)
	}
}
