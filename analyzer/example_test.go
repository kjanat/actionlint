package analyzer_test

import (
	"context"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/analysis"
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
)

type sudoAnalyzer struct{}

func (sudoAnalyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "no-sudo", Requires: []analyzer.Fact{analyzer.Scripts}}
}

func (sudoAnalyzer) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	var findings []diagnostic.Diagnostic
	for _, script := range input.Scripts {
		if strings.Contains(script.Text, "sudo ") {
			findings = append(findings, diagnostic.Diagnostic{Message: fmt.Sprintf("step %q runs sudo", script.StepID), Start: script.Start})
		}
	}
	return findings, nil
}

func ExampleAnalyzer() {
	source := []byte(`on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: install
        run: sudo apt-get install -y jq
`)
	result, err := analysis.Analyze(context.Background(), analysis.Request{
		Sources: []analysis.Source{{Path: "workflow.yml", Content: source}},
		Options: analysis.Options{Analyzers: []analyzer.Registration{{Analyzer: sudoAnalyzer{}}}},
	})
	if err != nil {
		fmt.Println("incomplete:", err)
	}
	for _, finding := range result.Diagnostics {
		fmt.Printf("%s:%d:%d %s [%s]\n", finding.Path, finding.Start.Line, finding.Start.Column, finding.Message, finding.Rule)
	}
	// Output:
	// workflow.yml:7:14 step "install" runs sudo [no-sudo]
}
