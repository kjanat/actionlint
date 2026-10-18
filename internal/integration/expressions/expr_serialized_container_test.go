package expressions_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/workspace"
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
			"toJSON(" + path + " == null)",
			"toJSON(!" + path + ")",
			"contains(toJSON(" + path + "), 'safe')",
			"toJSON(false && " + path + ")",
			"toJSON(" + path + ") == '{}'",
			"!toJSON(" + path + ")",
			"toJSON(" + path + ") && 'safe'",
			"case(toJSON(" + path + ") != '', 'safe', 'fallback')",
		} {
			t.Run(expression, func(t *testing.T) { checkReferencedWorkflowExpression(t, expression, false) })
		}
		// Without a non-string scalar proof, JSON may introduce quote delimiters
		// even without attacker control; pass it through the environment.
		for _, expression := range []string{
			"toJSON(" + path + " && false)",
			"toJSON('safe' || " + path + ")",
			"toJSON(case(false, " + path + ", 'safe'))",
		} {
			t.Run(expression, func(t *testing.T) { checkReferencedWorkflowExpression(t, expression, false, true) })
		}
		workflow := `on: {workflow_run: {workflows: [Build], types: [completed]}}
jobs:
  report:
    runs-on: ubuntu-latest
    if: contains(toJSON(` + path + `), 'safe')
    steps:
      - env:
          JSON: ${{ toJSON(` + path + `) }}
        run: printf '%s\n' "$JSON"
`
		result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(workflow)}})
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
			syntax := path != "head_repository.id" && path != "head_repository.owner.id" && path != "actor.id" && path != "actor.site_admin"
			checkReferencedWorkflowExpression(t, "toJSON(github.event.workflow_run."+path+")", false, syntax)
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
			`      - run: |
          echo '${{ toJSON(github.event.workflow_run.head_repository) }}'
`,
			`      - uses: actions/github-script@v8
        with:
          script: |
            console.log('${{ toJSON(github.event.workflow_run.head_repository) }}');
`,
		} {
			source := `on: {workflow_run: {workflows: [Build], types: [completed]}}
jobs:
  report:
    runs-on: ubuntu-latest
    steps:
` + script
			result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(strings.ReplaceAll(source, "\n", ending))}})
			wantLine := 7
			if strings.Contains(script, "uses:") {
				wantLine = 9
			}
			if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "script-injection" || result.Diagnostics[0].Path != "workflow.yaml" || result.Diagnostics[0].Start.Line != wantLine {
				t.Fatalf("missing container injection at %d: %+v, %v", wantLine, result.Diagnostics, err)
			}
		}
	}
}
