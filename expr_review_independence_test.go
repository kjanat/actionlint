package actionlint

import (
	"strings"
	"testing"
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
			checkReferencedWorkflowExpression(t, expression, false)
		})
	}
	workflow := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n      - env:\n          REF: ${{ github.event.workflow_run.referenced_workflows[0].ref }}\n          PATH_VALUE: ${{ github.event.workflow_run.referenced_workflows[0].path }}\n        run: printf '%s\\n' \"$REF\" \"$PATH_VALUE\"\n"
	result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(workflow)}}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("safe environment binding rejected: %v, %+v", err, result)
	}
}

func checkReferencedWorkflowExpression(t *testing.T, expression string, untrusted bool) {
	t.Helper()
	workflow := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo \"${{ " + expression + " }}\"\n"
	result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(workflow)}}})
	if err != nil {
		t.Fatal(err)
	}
	if !untrusted {
		if len(result.Diagnostics) != 0 {
			t.Fatalf("safe expression rejected: %+v", result.Diagnostics)
		}
		return
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "expression" || result.Diagnostics[0].Start.Line != 6 || !strings.Contains(result.Diagnostics[0].Message, "potentially untrusted") {
		t.Fatalf("missing injection finding: %+v", result.Diagnostics)
	}
}

func TestUnsoundTernaryIndependentExpressionErrors(t *testing.T) {
	for _, expression := range []string{
		"secrets.UNKNOWN && '' || 'fallback'",
		"github.event.issue.title && '' || 'fallback'",
	} {
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
				cfg, err := ParseConfig([]byte("lint: {rules: {correctness: {expression: " + levels.expression + ", unsound-ternary: " + levels.ternary + "}}}"))
				if err != nil {
					t.Fatal(err)
				}
				workflow := "on: push\nrun-name: ${{ " + expression + " }}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
				if strings.Contains(expression, "issue.title") {
					workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo \"${{ " + expression + " }}\"\n"
				}
				result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(workflow), Config: cfg}}})
				if err != nil {
					t.Fatal(err)
				}
				found := map[string]int{}
				for _, diagnostic := range result.Diagnostics {
					found[diagnostic.Rule]++
				}
				if (found["expression"] != 0) != levels.wantExpression || (found["unsound-ternary"] != 0) != levels.wantTernary || len(found) > 2 {
					t.Fatalf("independent rule levels lost: %+v", result.Diagnostics)
				}
			})
		}
	}
}
