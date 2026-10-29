package githubaction

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
	"actionlint.kjanat.dev/v2/internal/render"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/sys/windows"
)

func TestCLIAndActionShortWorkspaceIdentity(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace with a long name")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	longPath, err := windows.UTF16PtrFromString(workspace)
	if err != nil {
		t.Fatal(err)
	}
	size, err := windows.GetShortPathName(longPath, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, size)
	length, err := windows.GetShortPathName(longPath, &buffer[0], size)
	if err != nil || length >= size {
		t.Fatalf("resolve short workspace path: length=%d size=%d error=%v", length, size, err)
	}
	shortPath := windows.UTF16ToString(buffer[:length])
	if shortPath == workspace {
		t.Skip("volume does not generate short filenames")
	}
	t.Chdir(shortPath)
	const workflow = "on: push\njobs:\n  test:\n    runs-on: invalid-runner\n    steps:\n      - run: echo ok\n"
	if err := os.WriteFile("ci.yml", []byte(workflow), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	command := cli.Command{Stdout: &stdout, Stderr: &stderr}
	if code := command.Main([]string{"actionlint", "check", "--json", "--shellcheck=", "--pyflakes=", "ci.yml"}); code != 1 {
		t.Fatalf("CLI status %d: %s", code, &stderr)
	}
	var cliResult render.CheckResult
	if err := json.Unmarshal(stdout.Bytes(), &cliResult); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(workspace, "result.json")
	env := map[string]string{
		"GITHUB_WORKSPACE": shortPath, "GITHUB_OUTPUT": filepath.Join(workspace, "outputs"),
		"ACTIONLINT_ACTION_RESULT": resultPath, "INPUT_FILES": "ci.yml", "INPUT_FORMAT": "json",
		"INPUT_SHELLCHECK": "false", "INPUT_PYFLAKES": "false", "INPUT_FAIL-ON-ERROR": "false",
	}
	stdout.Reset()
	if code := Main(func(key string) string { return env[key] }, &stdout); code != 0 {
		t.Fatalf("Action status %d: %s", code, &stdout)
	}
	var actionResult render.CheckResult
	if err := json.Unmarshal([]byte(read(t, resultPath)), &actionResult); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(cliResult, actionResult, documentValues); diff != "" {
		t.Fatalf("CLI and Action short-path identities differ: %s", diff)
	}
}
