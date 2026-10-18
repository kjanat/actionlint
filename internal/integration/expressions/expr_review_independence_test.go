package expressions_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestReferencedWorkflowInjectionSources(t *testing.T) {
	for _, field := range []string{"ref", "path"} {
		for _, expression := range []string{
			"github.event.workflow_run.referenced_workflows[0]." + field,
			"github['event']['workflow_run']['referenced_workflows'][0]['" + field + "']",
			strings.ToUpper("github.event.workflow_run.referenced_workflows[0]." + field),
			"join(github.event.workflow_run.referenced_workflows.*." + field + ", ',')",
		} {
			t.Run(expression, func(t *testing.T) {
				checkReferencedWorkflowExpression(t, expression, true)
			})
		}
	}
	for _, expression := range []string{
		"github.event.workflow_run.referenced_workflows[0].sha",
		"join(github.event.workflow_run.referenced_workflows.*.sha, ',')",
		"contains(github.event.workflow_run.referenced_workflows[0].ref, 'main')",
		"startsWith(github.event.workflow_run.referenced_workflows[0].path, 'owner/')",
	} {
		t.Run(expression, func(t *testing.T) {
			checkReferencedWorkflowExpression(t, expression, false, strings.HasSuffix(expression, ".sha") || strings.HasPrefix(expression, "join("))
		})
	}
	workflow := `on: {workflow_run: {workflows: [Build], types: [completed]}}
jobs:
  report:
    runs-on: ubuntu-latest
    steps:
      - env:
          REF: ${{ github.event.workflow_run.referenced_workflows[0].ref }}
          PATH_VALUE: ${{ github.event.workflow_run.referenced_workflows[0].path }}
        run: printf '%s\n' "$REF" "$PATH_VALUE"
`
	result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(workflow)}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("safe environment binding rejected: %v, %+v", err, result)
	}
}

func TestWorkflowRunActorDisplayNameInjection(t *testing.T) {
	checkReferencedWorkflowExpression(t, "github.event.issue.title", true)
	checkReferencedWorkflowExpression(t, "github.event['ISSUE']['TITLE']", true)
	for _, actor := range []string{"actor", "triggering_actor"} {
		for _, expression := range []string{
			"github.event.workflow_run." + actor + ".name",
			"github['event']['workflow_run']['" + actor + "']['name']",
			"github.event.workflow_run['" + strings.ToUpper(actor) + "']['NAME']",
			strings.ToUpper("github.event.workflow_run." + actor + ".name"),
			"format('{0}', github.event.workflow_run." + actor + ".name)",
		} {
			t.Run(expression, func(t *testing.T) { checkReferencedWorkflowExpression(t, expression, true) })
		}
		for _, field := range []string{"login", "id", "type"} {
			checkReferencedWorkflowExpression(t, "github.event.workflow_run."+actor+"."+field, false, field != "id")
			checkReferencedWorkflowExpression(t, "github.event.workflow_run['"+strings.ToUpper(actor)+"']['"+strings.ToUpper(field)+"']", false, field != "id")
		}
		checkReferencedWorkflowExpression(t, "contains(github.event.workflow_run."+actor+".name, 'trusted')", false)
		workflow := `on: {workflow_run: {workflows: [Build], types: [completed]}}
jobs:
  report:
    runs-on: ubuntu-latest
    steps:
      - env:
          ACTOR_NAME: ${{ github.event.workflow_run.` + actor + `.name }}
        run: printf '%s\n' "$ACTOR_NAME"
`
		result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(workflow)}})
		if err != nil || len(result.Diagnostics) != 0 {
			t.Fatalf("safe actor environment binding rejected: %v, %+v", err, result)
		}
	}
}

func checkReferencedWorkflowExpression(t *testing.T, expression string, untrusted bool, syntaxSensitive ...bool) {
	t.Helper()
	workflow := `on: {issues: {}, workflow_run: {workflows: [Build], types: [completed]}}
jobs:
  report:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ ` + expression + " }}\"\n"
	result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(workflow)}})
	if err != nil {
		t.Fatal(err)
	}
	if !untrusted {
		if len(syntaxSensitive) != 0 && syntaxSensitive[0] {
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "script-interpolation" || result.Diagnostics[0].Start.Line != 6 {
				t.Fatalf("expected interpolation warning without an untrusted-input claim: %+v", result.Diagnostics)
			}
			return
		}
		if len(result.Diagnostics) != 0 {
			t.Fatalf("safe expression rejected: %+v", result.Diagnostics)
		}
		return
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "script-injection" || result.Diagnostics[0].Start.Line != 6 || !strings.Contains(result.Diagnostics[0].Message, "potentially untrusted") {
		t.Fatalf("missing injection finding: %+v", result.Diagnostics)
	}
}

func TestUnsoundTernaryIndependentExpressionErrors(t *testing.T) {
	for _, expression := range []string{
		"secrets.UNKNOWN && '' || 'fallback'",
		"github.event.issue.title && '' || 'fallback'",
	} {
		ruleName := "context-availability"
		if strings.Contains(expression, "issue.title") {
			ruleName = "script-injection"
		}
		for _, levels := range []struct {
			expression, ternary         string
			wantExpression, wantTernary bool
		}{
			{"off", "on", false, true},
			{"on", "on", true, true},
			{"on", "off", true, false},
			{"off", "off", false, false},
		} {
			t.Run(expression+"/"+levels.expression+"/"+levels.ternary, func(t *testing.T) {
				configText := "lint: {rules: {correctness: {unsound-ternary: " + levels.ternary + ", context-availability: " + levels.expression + "}, security: {script-injection: " + levels.expression + "}}}"
				cfg, err := repoconfig.Parse([]byte(configText))
				if err != nil {
					t.Fatal(err)
				}
				workflow := "on: push\nrun-name: ${{ " + expression + ` }}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
				if strings.Contains(expression, "issue.title") {
					workflow = `on: issues
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ ` + expression + " }}\"\n"
				}
				result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(workflow), Config: cfg}})
				if err != nil {
					t.Fatal(err)
				}
				found := map[string]int{}
				for _, diagnostic := range result.Diagnostics {
					found[diagnostic.Rule]++
				}
				if (found[ruleName] != 0) != levels.wantExpression || (found["unsound-ternary"] != 0) != levels.wantTernary || len(found) > 2 {
					t.Fatalf("independent rule levels lost: %+v", result.Diagnostics)
				}
			})
		}
	}
}
