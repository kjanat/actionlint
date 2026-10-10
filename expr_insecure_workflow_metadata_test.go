package actionlint

import (
	"io"
	"strings"
	"testing"
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
				{"      - run: |\n          echo '${{ " + expression + " }}'\n", 6},
				{"      - uses: actions/github-script@v8\n        with:\n          script: |\n            console.log('${{ " + expression + " }}')\n", 8},
			} {
				for _, ending := range []string{"\n", "\r\n"} {
					findings := lintSiblingWorkflowMetadata(t, strings.ReplaceAll(script.step, "\n", ending), ending)
					if len(findings) != 1 || findings[0].Kind != "expression" || findings[0].Line != script.line ||
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

func lintSiblingWorkflowMetadata(t *testing.T, step, ending string) []*Error {
	t.Helper()
	linter, err := NewLinter(io.Discard, &LinterOptions{WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ReplaceAll("on: {workflow_run: {workflows: ['Build*'], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n", "\n", ending) + step
	findings, err := linter.Lint("consumer.yaml", []byte(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	return findings
}
