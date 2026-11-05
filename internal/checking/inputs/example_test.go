package inputs_test

import (
	"context"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	workspace "actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/results"
)

type sudoAnalyzer struct{}

func (sudoAnalyzer) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "no-sudo", Category: "conventions", RequiredInputs: []checkinputs.InputKind{checkinputs.Scripts}}
}

func (sudoAnalyzer) CheckDocument(_ context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	var findings []results.Diagnostic
	for _, script := range input.Scripts {
		if strings.Contains(script.Text, "sudo ") {
			findings = append(findings, results.Diagnostic{Message: fmt.Sprintf("step %q runs sudo", script.StepID), Start: script.Start})
		}
	}
	return findings, nil
}

func Example_checker() {
	source := []byte(`on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: install
        run: sudo apt-get install -y jq
`)
	result, err := checking.Analyze(context.Background(),

		checking.Options{Analyzers: []checkinputs.Registration{{Checker: sudoAnalyzer{}}}}, []workspace.Source{{Path: "workflow.yml", Content: source}},
	)
	if err != nil {
		fmt.Println("incomplete:", err)
	}
	for _, finding := range result.Diagnostics {
		fmt.Printf("%s:%d:%d %s [%s]\n", finding.Path, finding.Start.Line, finding.Start.Column, finding.Message, finding.Rule)
	}
	// Output:
	// workflow.yml:7:14 step "install" runs sudo [no-sudo]
}
