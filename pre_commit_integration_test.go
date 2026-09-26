package actionlint

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestPreCommitDeletionOnlyIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("requires external Git and pre-commit processes")
	}
	for _, executable := range []string{"pre-commit", "git", "sh"} {
		if _, err := exec.LookPath(executable); err != nil {
			t.Skipf("integration test requires %s: %v", executable, err)
		}
	}
	var hook preCommitHook
	for _, candidate := range readPreCommitHooks(t) {
		if candidate.ID == "actionlint-system" {
			hook = candidate
			break
		}
	}
	if hook.ID == "" {
		t.Fatal("actionlint-system hook is missing")
	}
	shellArgs := preCommitShellArgs(t, hook)
	// Git for Windows changes PATH during shell startup; select the fixture afterward.
	script := `PATH="$PWD/bin:$PATH"; ` + shellArgs[1]
	hook.Entry = "sh -c '" + strings.ReplaceAll(script, "'", "'\"'\"'") + "' --"
	type localHook struct {
		preCommitHook `yaml:",inline"`
		Name          string `yaml:"name"`
		Language      string `yaml:"language"`
	}
	type localRepo struct {
		Repo  string      `yaml:"repo"`
		Hooks []localHook `yaml:"hooks"`
	}
	control := hook
	control.ID = "actionlint-filter-control"
	control.AlwaysRun = false
	config := struct {
		Repos []localRepo `yaml:"repos"`
	}{
		Repos: []localRepo{
			{Repo: "local", Hooks: []localHook{
				{preCommitHook: hook, Name: "actionlint integration", Language: "system"},
				{preCommitHook: control, Name: "deletion filtering control", Language: "system"},
			}},
		},
	}
	configBytes, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, deleted := range []string{"action.yml", ".github/actionlint.yaml", ".github/workflows/callee.yml"} {
		t.Run(deleted, func(t *testing.T) {
			root := t.TempDir()
			env := append(os.Environ(), "PRE_COMMIT_HOME="+t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
			write := func(name, content string, mode os.FileMode) {
				t.Helper()
				path := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), mode); err != nil {
					t.Fatal(err)
				}
			}
			run := func(executable string, args ...string) string {
				t.Helper()
				cmd := exec.Command(executable, args...)
				cmd.Dir = root
				cmd.Env = env
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%s %v: %v\n%s", executable, args, err, output)
				}
				return string(output)
			}
			write(".pre-commit-config.yaml", string(configBytes), 0o644)
			write(".github/workflows/ci.yml", "on: push\njobs:\n  call:\n    uses: ./.github/workflows/callee.yml\n", 0o644)
			write(".github/workflows/callee.yml", "on: workflow_call\n", 0o644)
			write("action.yml", "name: Fixture\n", 0o644)
			write(".github/actionlint.yaml", "{}\n", 0o644)
			write("bin/actionlint", "#!/bin/sh\nprintf '%s\\n' \"$#\" > hook-invoked\n", 0o755)
			run("git", "init", "--quiet")
			run("git", "add", ".")
			run("git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath="+t.TempDir(), "commit", "--quiet", "-m", "Fixture")
			run("git", "rm", "--quiet", "--", deleted)
			if staged := strings.TrimSpace(run("git", "diff", "--cached", "--name-status")); staged != "D\t"+deleted {
				t.Fatalf("expected only deletion %q staged, got %q", deleted, staged)
			}
			if selected := strings.TrimSpace(run("git", "diff", "--cached", "--name-only", "--diff-filter=ACMRTUXB")); selected != "" {
				t.Fatalf("expected no surviving staged files, got %q", selected)
			}
			marker := filepath.Join(root, "hook-invoked")
			output := run("pre-commit", "run", control.ID, "--verbose")
			if !strings.Contains(output, "Skipped") {
				t.Fatalf("control should skip deleted files: %s", output)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("control unexpectedly invoked actionlint: %v", err)
			}
			run("pre-commit", "run", hook.ID, "--verbose")
			invoked, err := os.ReadFile(marker)
			if err != nil {
				t.Fatalf("deletion-only commit did not invoke actionlint: %v", err)
			}
			if string(invoked) != "0\n" {
				t.Fatalf("expected invocation without filenames, got %q", invoked)
			}
		})
	}
}
