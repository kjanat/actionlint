package actionlint

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mattn/go-shellwords"
	"go.yaml.in/yaml/v4"
)

type preCommitHook struct {
	ID            string `yaml:"id"`
	PassFilenames *bool  `yaml:"pass_filenames"`
	AlwaysRun     bool   `yaml:"always_run"`
	Entry         string `yaml:"entry"`
}

func readPreCommitHooks(t *testing.T) []preCommitHook {
	t.Helper()
	content, err := os.ReadFile(".pre-commit-hooks.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var hooks []preCommitHook
	if err := yaml.Unmarshal(content, &hooks); err != nil {
		t.Fatal(err)
	}
	return hooks
}

func preCommitShellArgs(t *testing.T, hook preCommitHook) []string {
	t.Helper()
	args, err := shellwords.Parse(hook.Entry)
	if err != nil {
		t.Fatal(err)
	}
	if hook.ID == "actionlint-docker" {
		if len(args) < 3 || args[0] != "--entrypoint" || args[1] != "sh" || args[2] != "ghcr.io/kjanat/actionlint:1.17.0" {
			t.Fatalf("unexpected Docker entrypoint: %v", args)
		}
		args = append([]string{"sh"}, args[3:]...)
	}
	if len(args) != 4 || args[0] != "sh" || args[1] != "-c" || args[3] != "--" {
		t.Fatalf("unexpected shell entrypoint: %v", args)
	}
	return args[1:]
}

func TestPreCommitEmptyRepositoriesAndInvocation(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX sh is required by the host hooks")
	}
	for _, hook := range readPreCommitHooks(t) {
		args := preCommitShellArgs(t, hook)
		for _, kind := range []string{"action-only", "empty-workflows", "non-yaml", "workflow", "nested-workflow", "find-error"} {
			t.Run(hook.ID+"/"+kind, func(t *testing.T) {
				root := t.TempDir()
				bin := filepath.Join(root, "bin")
				if err := os.Mkdir(bin, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(bin, "actionlint"), []byte("#!/bin/sh\nprintf 'ARG:<%s>\\n' \"$@\"\nexit 7\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				write := func(name, content string) {
					t.Helper()
					path := filepath.Join(root, name)
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				write("action.yml", "name: Standalone\n")
				want := 0
				switch kind {
				case "empty-workflows", "find-error":
					if err := os.MkdirAll(filepath.Join(root, ".github/workflows"), 0o755); err != nil {
						t.Fatal(err)
					}
				case "non-yaml":
					write(".github/workflows/README.md", "No workflows\n")
				case "workflow":
					write(".github/workflows/ci.yml", "on: push\n")
					want = 7
				case "nested-workflow":
					write(".github/workflows/nested space/ci.yaml", "on: push\n")
					want = 7
				}
				if kind == "find-error" {
					if err := os.WriteFile(filepath.Join(bin, "find"), []byte("#!/bin/sh\nexit 9\n"), 0o755); err != nil {
						t.Fatal(err)
					}
					want = 9
				}
				shellArgs := slices.Clone(args)
				// Git for Windows prepends its tools when sh starts; install fixture overrides inside the shell.
				shellArgs[1] = `PATH="$PWD/bin:$PATH"; ` + shellArgs[1]
				cmd := exec.Command(sh, append(shellArgs, "-ignore", "message with spaces")...)
				cmd.Dir = root
				output, err := cmd.CombinedOutput()
				if err != nil && cmd.ProcessState == nil {
					t.Fatal(err)
				}
				if code := cmd.ProcessState.ExitCode(); code != want {
					t.Fatalf("exit=%d, want %d: %s", code, want, output)
				}
				if want == 7 {
					if string(output) != "ARG:<-ignore>\nARG:<message with spaces>\n" {
						t.Fatalf("arguments were not preserved: %s", output)
					}
				} else if len(output) != 0 {
					t.Fatalf("actionlint should not run: %s", output)
				}
			})
		}
	}
}

func TestPreCommitHookManifest(t *testing.T) {
	hooks := readPreCommitHooks(t)
	var ids []string
	for _, hook := range hooks {
		ids = append(ids, hook.ID)
		t.Run(hook.ID, func(t *testing.T) {
			if hook.PassFilenames == nil || *hook.PassFilenames {
				t.Fatal("hook must explicitly disable filename arguments to recheck all workflows")
			}
			if !hook.AlwaysRun {
				t.Fatal("hook must run even when pre-commit supplies no files after a deletion")
			}

		})
	}
	want := []string{"actionlint", "actionlint-docker", "actionlint-system", "actionlint-shellcheck"}
	if !slices.Equal(ids, want) {
		t.Fatalf("hook inventory changed: %v, want %v", ids, want)
	}
}

func TestPreCommitDependencyChangesRecheckWorkflows(t *testing.T) {
	hooks := readPreCommitHooks(t)
	for _, hook := range hooks {
		for _, kind := range []string{"action.yml", "action.yaml", "actionlint.yml", "actionlint.yaml", "workflow", "deleted-callee"} {
			t.Run(hook.ID+"/"+kind, func(t *testing.T) {
				root := t.TempDir()
				t.Chdir(root)
				write := func(path, content string) {
					t.Helper()
					path = filepath.Join(root, path)
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				write(".git/HEAD", "ref: refs/heads/main\n")
				actionPath := ".github/actions/build/action.yml"
				if kind == "action.yaml" {
					actionPath = ".github/actions/build/action.yaml"
				}
				const action = "name: Build\ndescription: Build fixture\nruns:\n  using: composite\n  steps:\n    - run: echo ok\n      shell: bash\n"
				const workflow = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./.github/actions/build\n"
				write(actionPath, action)
				write(".github/workflows/ci.yml", workflow)
				if kind == "deleted-callee" {
					write(".github/workflows/callee.yml", "on: workflow_call\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n")
					write(".github/workflows/ci.yml", "on: push\njobs:\n  build:\n    uses: ./.github/workflows/callee.yml\n")
				}
				run := func(args []string) (int, string) {
					var output bytes.Buffer
					cmd := Command{Stdin: strings.NewReader(""), Stdout: &output, Stderr: &output}
					code := cmd.Main(append([]string{"actionlint", "-shellcheck=", "-pyflakes=", "-no-color"}, args...))
					return code, output.String()
				}
				if code, output := run(nil); code != 0 {
					t.Fatalf("valid baseline failed (%d): %s", code, output)
				}
				changed := actionPath
				want := "name is required"
				switch kind {
				case "deleted-callee":
					changed = ".github/workflows/callee.yml"
					if err := os.Remove(filepath.Join(root, changed)); err != nil {
						t.Fatal(err)
					}
					want = "could not read reusable workflow file"
				case "actionlint.yml", "actionlint.yaml":
					changed = ".github/" + kind
					write(changed, "policy:\n  require-job-timeout: true\n")
					want = "[require-job-timeout]"
				case "workflow":
					changed = ".github/workflows/ci.yml"
					write(changed, strings.ReplaceAll(workflow, "ubuntu-latest", "invalid-runner-label"))
					want = "[runner-label]"
				default:
					write(changed, strings.TrimPrefix(action, "name: Build\n"))
				}
				var args []string
				if hook.PassFilenames == nil || *hook.PassFilenames {
					args = append(args, changed)
				}
				if code, output := run(args); code != 1 || !strings.Contains(output, want) || !strings.Contains(output, "ci.yml:") {
					t.Fatalf("expected workflow diagnostic %q after changing only %s; exit=%d: %s", want, changed, code, output)
				}
			})
		}
	}
}
