package engine

import (
	"strings"
	"testing"

	findingmodel "actionlint.kjanat.dev/internal/finding"
)

func TestWorkflowRunSiblingWorkflowMetadataInjection(t *testing.T) {
	for _, expression := range []string{
		"github.event.workflow.name",
		"github.event.workflow.path",
		"github['event']['workflow']['name']",
		"GITHUB.EVENT.WORKFLOW.PATH",
		"github.event.workflow['NAME']",
		"format('{0}', github.event.workflow.path)",
		"toJSON(github.event.workflow)",
		"join(github.event.workflow.*, ',')",
	} {
		t.Run(expression, func(t *testing.T) {
			for _, script := range []struct {
				step string
				line int
			}{
				{"      - run: |\n          echo '${{ " + expression + " }}'\n", 7},
				{`      - uses: actions/github-script@v8
        with:
          script: |
            console.log('${{ ` + expression + " }}')\n", 9},
			} {
				for _, ending := range []string{"\n", "\r\n"} {
					findings := lintSiblingWorkflowMetadata(t, strings.ReplaceAll(script.step, "\n", ending), ending)
					if len(findings) != 1 || findings[0].Rule != "script-injection" || findings[0].Start.Line != script.line ||
						!strings.Contains(findings[0].Message, "potentially untrusted") || !strings.Contains(findings[0].Message, "github.event.workflow") {
						t.Fatalf("missing located injection finding for %s: %+v", expression, findings)
					}
				}
			}
		})
	}
}

func TestWorkflowRunSiblingWorkflowMetadataSafeControls(t *testing.T) {
	for _, expression := range []string{
		"github.event.workflow.id",
		"github.event.workflow.node_id",
		"github.event.workflow.state",
		"github.event.workflow.created_at",
		"github.event.workflow.updated_at",
		"github.event.workflow.url",
		"github.event.workflow.html_url",
		"github.workflow",
		"contains(github.event.workflow.name, 'Build')",
		"startsWith(github.event.workflow.path, '.github/workflows/')",
		"endsWith(github.event.workflow.path, '.yml')",
	} {
		t.Run(expression, func(t *testing.T) {
			findings := lintSiblingWorkflowMetadata(t, "      - run: echo '${{ "+expression+" }}'\n", "\n")
			if len(findings) != 0 {
				t.Fatalf("safe expression rejected: %+v", findings)
			}
		})
	}
	step := "      - if: github.event.workflow.name != ''\n" +
		"        env:\n" +
		"          WORKFLOW_NAME: ${{ github.event.workflow.name }}\n" +
		"          WORKFLOW_PATH: ${{ github.event.workflow.path }}\n" +
		"        run: printf '%s\\n' \"$WORKFLOW_NAME\" \"$WORKFLOW_PATH\"\n"
	if findings := lintSiblingWorkflowMetadata(t, step, "\n"); len(findings) != 0 {
		t.Fatalf("condition or environment binding rejected: %+v", findings)
	}
}

func lintSiblingWorkflowMetadata(t *testing.T, step, ending string) []*findingmodel.Finding {
	t.Helper()
	request := Request{WorkingDir: t.TempDir()}
	var err error

	source := strings.ReplaceAll(`on: {workflow_run: {workflows: ['Build*'], types: [completed]}}
jobs:
  report:
    runs-on: ubuntu-latest
    steps:
`, "\n", ending) + step
	request.Sources = []SourceUnit{{Path: "consumer.yaml", InputPath: "consumer.yaml", Content: []byte(source), Project: nil}}
	findings, err := testFindings(runTestAnalysis(t.Context(), request))
	if err != nil {
		t.Fatal(err)
	}
	return findings
}
