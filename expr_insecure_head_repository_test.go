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

func TestWorkflowRunUserProfileInjection(t *testing.T) {
	for _, path := range []string{"head_repository.owner.name", "head_repository.owner.email", "actor.email", "triggering_actor.email"} {
		parts := strings.Split(path, ".")
		brackets := "github.event.workflow_run['" + strings.Join(parts, "']['") + "']"
		for _, expression := range []string{
			"github.event.workflow_run." + path,
			brackets,
			"github.event.workflow_run['" + strings.ToUpper(strings.Join(parts, "']['")) + "']",
			strings.ToUpper("github.event.workflow_run." + path),
			"format('{0}', github.event.workflow_run." + path + ")",
			"toJSON(github.event.workflow_run." + path + ")",
		} {
			t.Run(expression, func(t *testing.T) { checkReferencedWorkflowExpression(t, expression, true) })
		}
		for _, expression := range []string{
			"contains(github.event.workflow_run." + path + ", 'trusted')",
			"startsWith(github.event.workflow_run." + path + ", 'trusted')",
			"endsWith(github.event.workflow_run." + path + ", 'trusted')",
		} {
			t.Run(expression, func(t *testing.T) { checkReferencedWorkflowExpression(t, expression, false) })
		}
		workflow := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    if: github.event.workflow_run." + path + " == 'trusted'\n    steps:\n      - env:\n          PROFILE: ${{ github.event.workflow_run." + path + " }}\n        run: printf '%s\\n' \"$PROFILE\"\n"
		result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(workflow)}}})
		if err != nil || len(result.Diagnostics) != 0 {
			t.Fatalf("safe condition and environment binding for %s rejected: %v, %+v", path, err, result)
		}
	}
	for _, owner := range []string{"head_repository.owner", "actor", "triggering_actor"} {
		for _, field := range []string{"login", "id", "node_id", "type", "site_admin", "url", "html_url", "avatar_url"} {
			path := owner + "." + field
			t.Run(path, func(t *testing.T) {
				checkReferencedWorkflowExpression(t, "github.event.workflow_run."+path, false)
				checkReferencedWorkflowExpression(t, "github.event.workflow_run['"+strings.ToUpper(strings.ReplaceAll(path, ".", "']['"))+"']", false)
			})
		}
	}
}

func TestWorkflowRunUserProfileObjectFilters(t *testing.T) {
	for _, expression := range []string{
		"join(github.event.workflow_run.head_repository.*.name, ',')",
		"join(github.event.workflow_run.head_repository.*.email, ',')",
		"join(github.event.workflow_run.head_repository.owner.*, ',')",
		"toJSON(github.event.workflow_run.head_repository.owner.*)",
		"join(github.event.workflow_run.actor.*, ',')",
		"join(github.event.workflow_run.triggering_actor.*, ',')",
		"join(github.event.workflow_run.*.email, ',')",
	} {
		t.Run(expression, func(t *testing.T) { checkReferencedWorkflowExpression(t, expression, true) })
	}
	for _, expression := range []string{
		"join(github.event.workflow_run.head_repository.*.login, ',')",
		"join(github.event.workflow_run.*.login, ',')",
	} {
		t.Run(expression, func(t *testing.T) { checkReferencedWorkflowExpression(t, expression, false) })
	}
}

func TestWorkflowRunUserProfileScriptLocations(t *testing.T) {
	for _, path := range []string{"head_repository.owner.name", "head_repository.owner.email", "actor.email", "triggering_actor.email"} {
		for _, script := range []struct {
			step string
			line int
		}{
			{"      - run: |\n          echo '${{ github.event.workflow_run." + path + " }}'\n", 6},
			{"      - uses: actions/github-script@v8\n        with:\n          script: |\n            console.log('${{ github.event.workflow_run." + path + " }}')\n", 8},
		} {
			for _, ending := range []string{"\n", "\r\n"} {
				workflow := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n" + script.step
				result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(strings.ReplaceAll(workflow, "\n", ending))}}})
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "expression" || result.Diagnostics[0].Start.Line != script.line || !strings.Contains(result.Diagnostics[0].Message, "github.event.workflow_run."+path) {
					t.Fatalf("missing located injection for %s with %q: %+v", path, ending, result.Diagnostics)
				}
			}
		}
	}
}
