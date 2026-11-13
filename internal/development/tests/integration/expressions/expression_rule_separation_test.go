package expressions_test

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

const separatedExpressionWorkflow = `on: issues
jobs:
  build:
    if: secrets.TOKEN
    runs-on: ubuntu-latest
    env:
      REFERENCE: ${{ missing }}
      PAYLOAD: ${{ github.event.issue.titel }}
      TYPE: ${{ case(1, 'a', 'b') }}
      CALL: ${{ contains('x') }}
      SYNTAX: ${{ github.[ }}
    steps:
      - run: echo "${{ github.event.issue.title }}"
permissions: {}
`

func TestExpressionRuleSelectionIsIndependent(t *testing.T) {
	t.Parallel()
	rules := []string{"expression-reference", "event-payload", "expression-type", "expression-call", "expression-syntax", "context-availability", "script-injection"}
	for _, selected := range rules {
		t.Run(selected, func(t *testing.T) {
			t.Parallel()
			descriptor, ok := checking.ConfigurationRules().FindRule(selected)
			if !ok {
				t.Fatal("missing descriptor")
			}
			cfg, err := checking.ConfigurationRules().Parse(fmt.Appendf(nil, "lint: {rules: {preset: none, %s: {%s: warn}}}", descriptor.Group, selected))
			if err != nil {
				t.Fatal(err)
			}
			result, err := checking.Analyze(t.Context(), checking.Options{}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(separatedExpressionWorkflow), Config: cfg}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) == 0 {
				t.Fatal("selected rule did not run")
			}
			for _, d := range result.Diagnostics {
				if d.Rule != selected || d.Code == "" || d.Severity != "warning" || d.Help == "" || !strings.HasSuffix(d.DocumentationURL, "#"+selected) {
					t.Fatalf("selection or identity lost: %+v", d)
				}
			}
		})
	}
}

func TestPayloadSuppressionDoesNotSuppressInjection(t *testing.T) {
	t.Parallel()
	content := strings.ReplaceAll(separatedExpressionWorkflow, "PAYLOAD: ${{ github.event.issue.titel }}", "PAYLOAD: ${{ github.event.issue.titel }} # actionlint:ignore event-payload -- test independent rules")
	result, err := checking.Analyze(t.Context(), checking.Options{}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(content)}})
	if err != nil {
		t.Fatal(err)
	}
	injection := false
	for _, d := range result.Diagnostics {
		if d.Rule == "event-payload" || d.Rule == "inline-suppression" {
			t.Fatalf("suppression failed: %+v", d)
		}
		injection = injection || d.Rule == "script-injection"
	}
	if !injection {
		t.Fatal("payload suppression disabled script injection")
	}
}

func TestUnusedFormatArgumentRuleSelection(t *testing.T) {
	t.Parallel()
	const source = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    env:\n      VALUE: ${{ format('{0}', 'used', 'unused') }}%s\n    steps: []\npermissions: {}\n"
	for _, tc := range []struct{ level, comment, severity string }{
		{"warn", "", "warning"},
		{"error", "", "error"},
		{"off", "", ""},
		{"warn", " # ignore[expression-call]", ""},
	} {
		t.Run(tc.level+tc.comment, func(t *testing.T) {
			t.Parallel()
			cfg, err := checking.ConfigurationRules().Parse(fmt.Appendf(nil, "lint: {rules: {preset: none, correctness: {expression-call: %s}}}", tc.level))
			if err != nil {
				t.Fatal(err)
			}
			content := fmt.Sprintf(source, tc.comment)
			result, err := checking.Analyze(t.Context(), checking.Options{}, []workspace.Source{{Path: "workflow.yml", Content: []byte(content), Config: cfg}})
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if tc.severity != "" {
				want = 1
			}
			if len(result.Diagnostics) != want {
				t.Fatalf("diagnostics=%+v, want %d", result.Diagnostics, want)
			}
			if want == 1 {
				finding := result.Diagnostics[0]
				if finding.Rule != "expression-call" || finding.Code != "format-placeholder" || finding.Severity != tc.severity || finding.Start.Line != 6 || !strings.Contains(finding.Message, "unused in the format string") {
					t.Fatalf("wrong finding: %+v", finding)
				}
			}
		})
	}
}
