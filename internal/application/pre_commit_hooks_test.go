package application_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
)

func TestPreCommitDependencyChangesRecheckWorkflows(t *testing.T) {
	for _, kind := range []string{"action.yml", "action.yaml", "actionlint.yml", "actionlint.yaml", "workflow", "deleted-callee"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			write := func(path, content string) { writeFixture(t, root, path, content) }
			writeGitRepository(t, root)
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
			run := func() []diagmodel.Diagnostic {
				t.Helper()
				session := application.Options{WorkingDir: root}
				var err error

				result, err := application.Check(t.Context(), session, application.Input{Kind: application.Repository, Directory: ""})
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
