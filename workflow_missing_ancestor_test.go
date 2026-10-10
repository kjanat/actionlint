package actionlint

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

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
				for _, name := range []string{"New", "Old", "Outside", "Missing"} {
					t.Run(roots[0]+name, func(t *testing.T) {
						project := &Project{root: roots[1]}
						producer := filepath.Join(roots[0], ".github/workflows/build.yml")
						consumer := "on: {workflow_run: {workflows: [" + name + `], types: [completed]}}
jobs:
  test:
    runs-on: ubuntu-latest
    steps: [{run: echo ok}]
`
						result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: roots[0], Sources: []SourceUnit{
							{Path: filepath.Join(roots[1], ".github/workflows/build.yml"), Content: []byte("name: Old\n" + commandGoodWorkflow), Project: project},
							{Path: producer, Content: []byte("name: New\n" + commandGoodWorkflow), Project: project},
							{Path: filepath.Join(base, "outside/.github/workflows/build.yml"), Content: []byte("name: Outside\n" + commandGoodWorkflow), Project: project},
							{Path: filepath.Join(roots[0], ".github/workflows/consumer.yml"), Content: []byte(consumer), Project: project},
						}})
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
			}
		})
	}
}
