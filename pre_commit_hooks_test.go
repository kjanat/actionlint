package actionlint

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

type preCommitHook struct {
	ID            string   `yaml:"id"`
	PassFilenames *bool    `yaml:"pass_filenames"`
	AlwaysRun     bool     `yaml:"always_run"`
	Files         string   `yaml:"files,omitempty"`
	Types         []string `yaml:"types,omitempty"`
	Entry         string   `yaml:"entry"`
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
			if hook.Files != "" || len(hook.Types) != 0 {
				t.Fatalf("hook must not filter files: files=%q types=%v", hook.Files, hook.Types)
			}
			if words := strings.Fields(hook.Entry); len(words) != 1 || !strings.HasSuffix(words[0], "actionlint") && !strings.Contains(words[0], "actionlint:") {
				t.Fatalf("entry must run actionlint without arguments: %q", hook.Entry)
			}
		})
	}
	want := []string{"actionlint", "actionlint-docker", "actionlint-system", "actionlint-bundled", "actionlint-shellcheck"}
	if !slices.Equal(ids, want) {
		t.Fatalf("hook inventory changed: %v, want %v", ids, want)
	}
}

func TestPreCommitDependencyChangesRecheckWorkflows(t *testing.T) {
	for _, kind := range []string{"action.yml", "action.yaml", "actionlint.yml", "actionlint.yaml", "workflow", "deleted-callee"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			write := func(path, content string) { writeShellcheckFixture(t, root, path, content) }
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
			run := func() []Diagnostic {
				t.Helper()
				session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
				if err != nil {
					t.Fatal(err)
				}
				result, err := session.Repository("")
				if err != nil {
					t.Fatal(err)
				}
				return result.Diagnostics
			}
			if diagnostics := run(); len(diagnostics) != 0 {
				t.Fatalf("valid baseline failed: %+v", diagnostics)
			}
			want := "name is required"
			switch kind {
			case "deleted-callee":
				if err := os.Remove(filepath.Join(root, ".github/workflows/callee.yml")); err != nil {
					t.Fatal(err)
				}
				want = "could not read reusable workflow file"
			case "actionlint.yml", "actionlint.yaml":
				write(".github/"+kind, "policy:\n  require-job-timeout: true\n")
				want = "timeout-minutes"
			case "workflow":
				write(".github/workflows/ci.yml", strings.ReplaceAll(workflow, "ubuntu-latest", "invalid-runner-label"))
				want = "invalid-runner-label"
			default:
				write(actionPath, strings.TrimPrefix(action, "name: Build\n"))
			}
			diagnostics := run()
			if !slices.ContainsFunc(diagnostics, func(d Diagnostic) bool {
				return strings.Contains(d.Message, want) && filepath.Base(d.Path) == "ci.yml"
			}) {
				t.Fatalf("expected a ci.yml diagnostic containing %q after changing a dependency: %+v", want, diagnostics)
			}
		})
	}
}
