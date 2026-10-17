package engine

import (
	"fmt"
	"strings"
	"testing"
)

const separatedExpressionWorkflow = `on: issues
jobs:
  build:
    if: secrets.TOKEN
    runs-on: ubuntu-latest
    env:
      REFERENCE: ${{ missing }}
      PAYLOAD: ${{ github.event.issue.titel }}
      TYPE: ${{ contains(github, 'x') }}
      CALL: ${{ contains('x') }}
      SYNTAX: ${{ github.[ }}
    steps:
      - run: echo "${{ github.event.issue.title }}"
`

func TestExpressionRuleSelectionIsIndependent(t *testing.T) {
	rules := []string{"expression-reference", "event-payload", "expression-type", "expression-call", "expression-syntax", "context-availability", "script-injection"}
	for _, selected := range rules {
		t.Run(selected, func(t *testing.T) {
			descriptor, ok := findRuleDescriptor(selected)
			if !ok {
				t.Fatal("missing descriptor")
			}
			cfg, err := ParseConfig(fmt.Appendf(nil, "lint: {rules: {preset: none, %s: {%s: warn}}}", descriptor.Group, selected))
			if err != nil {
				t.Fatal(err)
			}
			result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(separatedExpressionWorkflow), Config: cfg}}})
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
	content := strings.ReplaceAll(separatedExpressionWorkflow, "PAYLOAD: ${{ github.event.issue.titel }}", "PAYLOAD: ${{ github.event.issue.titel }} # actionlint:ignore event-payload -- test independent rules")
	result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(content)}}})
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
