package workflow_test

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/testutil"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestWorkflowRunSymlinkCheckout(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "checkout")
	testutil.WriteFixture(t, realRoot, ".github/workflows/build.yml", "name: [\n")
	alias := filepath.Join(base, "linked-checkout")
	if err := os.Symlink(realRoot, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	for _, roots := range [][2]string{{realRoot, alias}, {alias, realRoot}} {
		for _, name := range []string{"New", "Missing"} {
			t.Run(roots[0]+name, func(t *testing.T) {
				project := workspace.Identity(roots[1])
				consumer := "on: {workflow_run: {workflows: [" + name + `], types: [completed]}}
jobs:
  test:
    runs-on: ubuntu-latest
    steps: [{run: echo ok}]
`
				result, err := application.Analyze(t.Context(), application.Options{WorkingDir: roots[0]}, []workspace.Source{
					{Path: ".github/workflows/build.yml", Content: []byte("name: New\n" + testutil.GoodWorkflow), Project: project},
					{Path: ".github/workflows/consumer.yml", Content: []byte(consumer), Project: project},
				})
				want := 0
				if name == "Missing" {
					want = 1
				}
				if err != nil || len(result.Diagnostics) != want || want == 1 && result.Diagnostics[0].Rule != "workflow-run-names" {
					t.Fatalf("got %+v, %v; want %d findings", result.Diagnostics, err, want)
				}
				if !slices.Contains(result.Inputs, filepath.Join(roots[0], ".github/workflows/build.yml")) {
					t.Fatalf("original overlay path missing from inputs: %v", result.Inputs)
				}
			})
		}
	}
}

func TestWorkflowRunPatternLineBreaks(t *testing.T) {
	for _, pattern := range []string{"Build\nCI", "Build\rCI", "Build\r\nCI", "!Build\nCI", "Build\\\nCI", "\nBuild", "Build\r"} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(strconv.Quote(pattern)+strconv.Quote(ending), func(t *testing.T) {
				root := t.TempDir()
				source := `on:
  workflow_run:
    workflows: [` + strconv.Quote(pattern) + `]
    types: [completed]
jobs:
  test:
    runs-on: ubuntu-latest
    steps: [{run: echo ok}]
`
				result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, []workspace.Source{{Path: ".github/workflows/consumer.yml", Content: []byte(strings.ReplaceAll(source, "\n", ending)), Project: workspace.Identity(root)}})
				if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "glob" || result.Diagnostics[0].Start.Line != 3 {
					t.Fatalf("expected glob finding on line 3: %+v, %v", result.Diagnostics, err)
				}
			})
		}
	}
}

func TestWorkflowRunPatternScalarLineBreaks(t *testing.T) {
	for _, tc := range []struct {
		name, scalar string
		want         int
	}{
		{"literal expression", strconv.Quote("${{ 'Build\nCI' }}"), 1},
		{"negative literal expression", strconv.Quote("${{ '!Build\rCI' }}"), 1},
		{"literal block", "|-\n        Build\n        CI", 1},
		{"folded block", ">-\n        Build\n        CI", 0},
		{"escaped backslash", `'Build\nCI'`, 0},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+strconv.Quote(ending), func(t *testing.T) {
				root := t.TempDir()
				project := workspace.Identity(root)
				source := `on:
  workflow_run:
    workflows:
      - ` + tc.scalar + `
    types: [completed]
jobs:
  test:
    runs-on: ubuntu-latest
    steps: [{run: echo ok}]
`
				result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, []workspace.Source{
					{Path: ".github/workflows/consumer.yml", Content: []byte(strings.ReplaceAll(source, "\n", ending)), Project: project},
					{Path: ".github/workflows/build.yml", Content: []byte("name: Build CI\n" + testutil.GoodWorkflow), Project: project},
					{Path: ".github/workflows/backslash.yml", Content: []byte("name: 'Build\\nCI'\n" + testutil.GoodWorkflow), Project: project},
				})
				if err != nil || len(result.Diagnostics) != tc.want || tc.want > 0 && (result.Diagnostics[0].Rule != "glob" || result.Diagnostics[0].Start.Line != 4) {
					t.Fatalf("expected %d glob findings at line 4: %+v, %v", tc.want, result.Diagnostics, err)
				}
			})
		}
	}
}
