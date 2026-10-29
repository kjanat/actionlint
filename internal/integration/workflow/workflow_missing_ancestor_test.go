package workflow_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func workflowRunConsumer(name string) string {
	return "on: {workflow_run: {workflows: [" + name + `], types: [completed]}}
jobs:
  test:
    runs-on: ubuntu-latest
    steps: [{run: echo ok}]
`
}

func TestWorkflowRunMissingAncestorAlias(t *testing.T) {
	for _, existing := range []string{".", ".github", ".github/workflows"} {
		t.Run(existing, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "checkout")
			if err := os.MkdirAll(filepath.Join(root, existing), 0o755); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(base, "linked-checkout")
			if err := os.Symlink(root, alias); err != nil {
				t.Skipf("directory symlinks unavailable: %v", err)
			}
			for _, roots := range [][2]string{{root, alias}, {alias, root}} {
				project := workspace.Identity(roots[1])
				producer := filepath.Join(roots[0], ".github/workflows/build.yml")
				for _, name := range []string{"New", "Old", "Outside", "Missing"} {
					t.Run(roots[0]+name, func(t *testing.T) {
						result, err := application.Analyze(t.Context(), application.Options{WorkingDir: roots[0]}, []workspace.Source{
							{Path: producer, Content: []byte("name: New\n" + testutil.GoodWorkflow), Project: project},
							{Path: filepath.Join(base, "outside/.github/workflows/build.yml"), Content: []byte("name: Outside\n" + testutil.GoodWorkflow), Project: project},
							{Path: filepath.Join(roots[0], ".github/workflows/consumer.yml"), Content: []byte(workflowRunConsumer(name)), Project: project},
						})
						want := 1
						if name == "New" {
							want = 0
						}
						if err != nil || len(result.Diagnostics) != want || want == 1 && result.Diagnostics[0].Rule != "workflow-run-names" {
							t.Fatalf("got %+v, %v; want %d findings", result.Diagnostics, err, want)
						}
						if !slices.Contains(result.Inputs, producer) {
							t.Fatalf("original overlay path missing from inputs: %v", result.Inputs)
						}
					})
				}
				// The other spelling names the same document. Differing content
				// through both is conflicting source evidence, not a precedence.
				t.Run(roots[0]+"Conflict", func(t *testing.T) {
					_, err := application.Analyze(t.Context(), application.Options{WorkingDir: roots[0]}, []workspace.Source{
						{Path: filepath.Join(roots[1], ".github/workflows/build.yml"), Content: []byte("name: Old\n" + testutil.GoodWorkflow), Project: project},
						{Path: producer, Content: []byte("name: New\n" + testutil.GoodWorkflow), Project: project},
						{Path: filepath.Join(roots[0], ".github/workflows/consumer.yml"), Content: []byte(workflowRunConsumer("New")), Project: project},
					})
					if err == nil || !strings.Contains(err.Error(), "source-revision-conflict") || !strings.Contains(err.Error(), "conflicting overlays") {
						t.Fatalf("aliased overlays with different content were not a conflict: %v", err)
					}
				})
			}
		})
	}
}
