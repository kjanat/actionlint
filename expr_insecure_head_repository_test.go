package actionlint

import (
	"strings"
	"testing"
)

func TestExprInsecureHeadRepositoryMetadata(t *testing.T) {
	for _, property := range []string{"description", "homepage"} {
		for _, expression := range []string{
			"github.event.workflow_run.head_repository." + property,
			"github['event']['workflow_run']['head_repository']['" + property + "']",
			strings.ToUpper("github.event.workflow_run.head_repository." + property),
			"format('{0}', github.event.workflow_run.head_repository." + property + ")",
		} {
			t.Run(expression, func(t *testing.T) {
				workflow := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo \"${{ " + expression + " }}\"\n"
				result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(workflow)}}})
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "expression" || result.Diagnostics[0].Start.Line != 6 || !strings.Contains(result.Diagnostics[0].Message, "potentially untrusted") {
					t.Fatalf("missing script injection finding: %+v", result.Diagnostics)
				}
			})
		}
	}
}

func TestExprInsecureHeadRepositorySafeControls(t *testing.T) {
	for _, expression := range []string{
		"github.event.workflow_run.head_repository.id",
		"github.event.workflow_run.head_repository.name",
		"github.event.workflow_run.head_repository.full_name",
		"github.event.workflow_run.head_repository.private",
		"github.event.workflow_run.head_repository.html_url",
		"github.event.workflow_run.head_repository.url",
		"contains(github.event.workflow_run.head_repository.description, 'release')",
	} {
		t.Run(expression, func(t *testing.T) {
			workflow := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo \"${{ " + expression + " }}\"\n"
			result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(workflow)}}})
			if err != nil || len(result.Diagnostics) != 0 {
				t.Fatalf("safe expression rejected: %v, %+v", err, result)
			}
		})
	}
	workflow := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n      - env:\n          DESCRIPTION: ${{ github.event.workflow_run.head_repository.description }}\n          HOMEPAGE: ${{ github.event.workflow_run.head_repository.homepage }}\n        run: printf '%s\\n' \"$DESCRIPTION\" \"$HOMEPAGE\"\n"
	result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(workflow)}}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("environment binding rejected: %v, %+v", err, result)
	}
}
