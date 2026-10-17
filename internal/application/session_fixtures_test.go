package application

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/workspace"
)

func executableFixture(t *testing.T) (string, func(...string)) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git required")
	}
	root := t.TempDir()
	command := func(args ...string) {
		t.Helper()
		if out, err := workspace.GitCommand(t.Context(), git, root, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	command("init", "--quiet")
	command("config", "core.filemode", "false")
	for _, name := range []string{"bad.sh", "good.sh", "scripts/bad.sh", "scripts/good.sh", "space dir/bad.sh", "{good,bad}.sh"} {
		writeShellcheckFixture(t, root, name, "#!/bin/sh\necho script\n")
		command("add", "--", name)
		mode := "-x"
		if strings.HasSuffix(name, "good.sh") {
			mode = "+x"
		}
		command("update-index", "--chmod="+mode, "--", name)
	}
	writeShellcheckFixture(t, root, "untracked.sh", "#!/bin/sh\n")
	writeShellcheckFixture(t, root, "intent.sh", "#!/bin/sh\n")
	command("add", "-N", "intent.sh")
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false}\n")
	return root, command
}

func shellcheckForTest(t *testing.T) string {
	t.Helper()
	command, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("ShellCheck required")
	}
	t.Setenv("SHELLCHECK_OPTS", "")
	return command
}

func writeShellcheckFixture(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
