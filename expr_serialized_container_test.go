package actionlint

import (
	"strings"
	"testing"
)

func TestExprSerializedWorkflowRunContainers(t *testing.T) {
	for _, container := range []string{"head_repository", "actor", "triggering_actor", "referenced_workflows", "head_commit"} {
		path := "github.event.workflow_run." + container
		for _, expression := range []string{
			"toJSON(" + path + ")",
			"TOJSON(github.event.workflow_run['" + strings.ToUpper(container) + "'])",
			"format('{0}', toJSON(" + path + "))",
			"toJSON(github.event_name == 'workflow_run' && " + path + ")",
			"toJSON(" + path + " || 'fallback')",
			"toJSON(case(github.event_name == 'workflow_run', " + path + ", 'fallback'))",
		} {
			t.Run(expression, func(t *testing.T) { checkReferencedWorkflowExpression(t, expression, true) })
		}
		for _, expression := range []string{
			path,
			"toJSON(" + path + " == null)",
			"toJSON(!" + path + ")",
			"contains(toJSON(" + path + "), 'safe')",
			"toJSON(contains(" + path + ", 'safe'))",
			"toJSON(" + path + " && false)",
			"toJSON(false && " + path + ")",
			"toJSON('safe' || " + path + ")",
			"toJSON(case(false, " + path + ", 'safe'))",
			"toJSON(" + path + ") == '{}'",
			"!toJSON(" + path + ")",
			"toJSON(" + path + ") && 'safe'",
			"case(toJSON(" + path + "), 'safe', 'fallback')",
		} {
			t.Run(expression, func(t *testing.T) { checkReferencedWorkflowExpression(t, expression, false) })
		}
		workflow := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    if: contains(toJSON(" + path + "), 'safe')\n    steps:\n      - env:\n          JSON: ${{ toJSON(" + path + ") }}\n        run: printf '%s\\n' \"$JSON\"\n"
		result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(workflow)}}})
		if err != nil || len(result.Diagnostics) != 0 {
			t.Fatalf("safe condition/environment binding rejected: %+v, %v", result.Diagnostics, err)
		}
	}
}

func TestExprSerializedContainerSelections(t *testing.T) {
	for _, path := range []string{
		"head_repository.owner", "head_commit.author", "head_commit.committer",
		"referenced_workflows[0]", "referenced_workflows.*", "pull_requests.*.head",
	} {
		t.Run(path, func(t *testing.T) {
			checkReferencedWorkflowExpression(t, "toJSON(github.event.workflow_run."+path+")", true)
		})
	}
	for _, path := range []string{
		"head_repository.id", "head_repository.owner.login", "head_repository.owner.id",
		"actor.login", "actor.type", "actor.id", "actor.site_admin",
		"head_commit.id", "referenced_workflows.*.sha", "referenced_workflows[0].sha",
	} {
		t.Run(path, func(t *testing.T) {
			checkReferencedWorkflowExpression(t, "toJSON(github.event.workflow_run."+path+")", false)
		})
	}
}

func TestExprSerializedContainerPreservesLeafChecks(t *testing.T) {
	for _, expression := range []string{
		"github.event.workflow_run.actor.name == 'safe'",
		"!github.event.workflow_run.actor.name",
		"toJSON(github.event.workflow_run.actor.name) == 'safe'",
	} {
		t.Run(expression, func(t *testing.T) { checkReferencedWorkflowExpression(t, expression, true) })
	}
}

func TestExprSerializedContainerScriptSinks(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		for _, script := range []string{
			"      - run: |\n          echo '${{ toJSON(github.event.workflow_run.head_repository) }}'\n",
			"      - uses: actions/github-script@v8\n        with:\n          script: |\n            console.log('${{ toJSON(github.event.workflow_run.head_repository) }}');\n",
		} {
			source := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n" + script
			result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(strings.ReplaceAll(source, "\n", ending))}}})
			wantLine := 6
			if strings.Contains(script, "uses:") {
				wantLine = 8
			}
			if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "expression" || result.Diagnostics[0].Path != "workflow.yaml" || result.Diagnostics[0].Start.Line != wantLine {
				t.Fatalf("missing container injection at %d: %+v, %v", wantLine, result.Diagnostics, err)
			}
		}
	}
}
