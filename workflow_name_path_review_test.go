package actionlint

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestWorkflowRunSymlinkCheckout(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "checkout")
	writeShellcheckFixture(t, realRoot, ".github/workflows/build.yml", "name: [\n")
	alias := filepath.Join(base, "linked-checkout")
	if err := os.Symlink(realRoot, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	for _, roots := range [][2]string{{realRoot, alias}, {alias, realRoot}} {
		for _, name := range []string{"New", "Missing"} {
			t.Run(roots[0]+name, func(t *testing.T) {
				project := &Project{root: roots[1]}
				consumer := "on: {workflow_run: {workflows: [" + name + "], types: [completed]}}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"
				result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: roots[0], Sources: []SourceUnit{
					{Path: ".github/workflows/build.yml", Content: []byte("name: New\n" + commandGoodWorkflow), Project: project},
					{Path: ".github/workflows/consumer.yml", Content: []byte(consumer), Project: project},
				}})
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
				source := "on:\n  workflow_run:\n    workflows: [" + strconv.Quote(pattern) + "]\n    types: [completed]\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"
				result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: root, Sources: []SourceUnit{{Path: ".github/workflows/consumer.yml", Content: []byte(strings.ReplaceAll(source, "\n", ending)), Project: &Project{root: root}}}})
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
				project := &Project{root: root}
				source := "on:\n  workflow_run:\n    workflows:\n      - " + tc.scalar + "\n    types: [completed]\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"
				result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: root, Sources: []SourceUnit{
					{Path: ".github/workflows/consumer.yml", Content: []byte(strings.ReplaceAll(source, "\n", ending)), Project: project},
					{Path: ".github/workflows/build.yml", Content: []byte("name: Build CI\n" + commandGoodWorkflow), Project: project},
					{Path: ".github/workflows/backslash.yml", Content: []byte("name: 'Build\\nCI'\n" + commandGoodWorkflow), Project: project},
				}})
				if err != nil || len(result.Diagnostics) != tc.want || tc.want > 0 && (result.Diagnostics[0].Rule != "glob" || result.Diagnostics[0].Start.Line != 4) {
					t.Fatalf("expected %d glob findings at line 4: %+v, %v", tc.want, result.Diagnostics, err)
				}
			})
		}
	}
}
