package checking_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	repository "actionlint.kjanat.dev/v2/internal/model/repositories"
	diagmodel "actionlint.kjanat.dev/v2/internal/results"
)

func TestPreCommitDependencyChangesRecheckWorkflows(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"action.yml", "action.yaml", "actionlint.yml", "actionlint.yaml", "workflow", "deleted-callee"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			write := func(path, content string) { writeFixture(t, root, path, content) }
			writeGitRepository(t, root)
			labels, err := repository.NewNames()
			if err != nil {
				t.Fatal(err)
			}
			actionPath := ".github/actions/build/action.yml"
			if kind == "action.yaml" {
				actionPath = ".github/actions/build/action.yaml"
			}
			const action = "name: Build\ndescription: Build fixture\nruns:\n  using: composite\n  steps:\n    - run: echo ok\n      shell: bash\n"
			const workflow = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./.github/actions/build\npermissions: {}\n"
			write(actionPath, action)
			write(".github/workflows/ci.yml", workflow)
			if kind == "deleted-callee" {
				write(".github/workflows/callee.yml", "on: workflow_call\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\npermissions: {}\n")
				write(".github/workflows/ci.yml", "on: push\njobs:\n  build:\n    uses: ./.github/workflows/callee.yml\npermissions: {}\n")
			}
			run := func() []diagmodel.Diagnostic {
				t.Helper()
				session := checking.Options{WorkingDir: root}
				var err error
				session.Repository = func(context.Context, string) repository.Snapshot {
					return repository.Snapshot{RunnerLabels: labels}
				}

				result, err := checking.Check(t.Context(), session, inputmodel.Request{Kind: inputmodel.Repository, Directory: ""})
				if err != nil {
					t.Fatal(err)
				}
				return result.Diagnostics
			}
			if diagnostics := run(); len(diagnostics) != 0 {
				t.Fatalf("valid baseline failed: %+v", diagnostics)
			}
			want := "name is required"
			wantPath := ".github/workflows/ci.yml"
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
				wantPath = actionPath
			}
			diagnostics := run()
			if !slices.ContainsFunc(diagnostics, func(d diagmodel.Diagnostic) bool {
				return strings.Contains(d.Message, want) && filepath.ToSlash(d.Path) == wantPath
			}) {
				t.Fatalf("expected a %s diagnostic containing %q after changing a dependency: %+v", wantPath, want, diagnostics)
			}
		})
	}
}
