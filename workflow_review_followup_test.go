package actionlint

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWorkflowRunPullRequestHeadRefs(t *testing.T) {
	for _, expression := range []string{
		"github.event.workflow_run.pull_requests[0].head.ref",
		"github['event']['workflow_run']['pull_requests'][0]['head']['ref']",
		"GITHUB.EVENT.WORKFLOW_RUN.PULL_REQUESTS[0].HEAD.REF",
		"join(github.event.workflow_run.pull_requests.*.head.ref, ',')",
		"format('{0}', github.event.workflow_run.pull_requests[0].head.ref)",
	} {
		t.Run(expression, func(t *testing.T) {
			source := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo \"${{ " + expression + " }}\"\n"
			result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(source)}}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "expression" || !strings.Contains(result.Diagnostics[0].Message, "potentially untrusted") {
				t.Fatalf("missing untrusted head-ref finding: %+v", result.Diagnostics)
			}
		})
	}
	for _, expression := range []string{
		"github.event.workflow_run.pull_requests[0].head.sha",
		"github.event.workflow_run.pull_requests[0].head.repo.id",
		"github.event.workflow_run.pull_requests[0].number",
		"contains(github.event.workflow_run.pull_requests[0].head.ref, 'release')",
	} {
		source := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo \"${{ " + expression + " }}\"\n"
		result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(source)}}})
		if err != nil || len(result.Diagnostics) != 0 {
			t.Fatalf("safe expression %q: %v, %+v", expression, err, result)
		}
	}
	source := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n      - env:\n          HEAD_REF: ${{ github.event.workflow_run.pull_requests[0].head.ref }}\n        run: printf '%s\\n' \"$HEAD_REF\"\n"
	result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(source)}}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("environment binding rejected: %v, %+v", err, result)
	}
}

func TestWorkflowRunLiteralReferences(t *testing.T) {
	for _, tc := range []struct {
		producer, reference string
		want                int
	}{
		{"Build", "${{ 'Build' }}", 0},
		{"Build", "${{ 'Typo' }}", 1},
		{"Build", "${{ 'build' }}", 1},
		{"Build", "${{ 'Build*' }}", 0},
		{"Build", "${{ 'Missing*' }}", 1},
		{"Build's CI", "${{ 'Build''s CI' }}", 0},
		{"Build", "${{ '!Missing' }}", 0},
		{"Build", "${{ vars.WORKFLOW }}", 0},
		{"Build", "${{ format('Typo') }}", 0},
		{"Build", "${{ 'Typo' }} suffix", 0},
	} {
		t.Run(tc.producer+tc.reference, func(t *testing.T) {
			root := t.TempDir()
			producer := "name: " + strconv.Quote(tc.producer) + "\n" + commandGoodWorkflow
			writeShellcheckFixture(t, root, ".github/workflows/build.yml", producer)
			consumer := "on: {workflow_run: {workflows: [" + strconv.Quote(tc.reference) + "], types: [completed]}}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: root, Sources: []SourceUnit{{Path: filepath.Join(root, ".github/workflows/consumer.yml"), Content: []byte(consumer), Project: &Project{root: root}}}})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "workflow-run-names" {
					count++
					if diagnostic.Start.Line != 1 || tc.reference == "${{ 'Typo' }}" && !strings.Contains(diagnostic.Message, "Typo") {
						t.Fatalf("incorrect literal reference diagnostic: %+v", diagnostic)
					}
				}
			}
			if count != tc.want {
				t.Fatalf("got %d workflow-name findings, want %d: %+v", count, tc.want, result.Diagnostics)
			}
		})
	}
}
