package analyzer_test

import (
	"context"
	"fmt"

	"actionlint.kjanat.dev/v2/analysis"
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/syntax"
)

// stepNameAnalyzer checks a document without depending on the traversal host.
type stepNameAnalyzer struct{}

func (stepNameAnalyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "step-name", Description: "Checks every step has a name", Requires: []analyzer.Fact{analyzer.Source}}
}

func (stepNameAnalyzer) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	if input.Kind != "workflow" {
		return nil, nil
	}
	workflow, _ := syntax.Parse([]byte(input.Content))
	if workflow == nil {
		return nil, nil
	}
	var findings []diagnostic.Diagnostic
	var checkSteps func([]*syntax.Step)
	checkSteps = func(steps []*syntax.Step) {
		for _, step := range steps {
			if step.Name == nil {
				findings = append(findings, diagnostic.Diagnostic{
					Message: "every step must have its name",
					Start:   diagnostic.Position{Line: step.Pos.Line, Column: step.Pos.Col},
				})
			}
			if parallel, ok := step.Exec.(*syntax.ExecParallel); ok {
				checkSteps(parallel.Steps)
			}
		}
	}
	for _, job := range workflow.Jobs {
		checkSteps(job.Steps)
	}
	return findings, nil
}

func ExampleAnalyzer_stepNames() {
	source := []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`)
	result, err := analysis.Analyze(context.Background(), analysis.Request{
		Sources: []analysis.Source{{Path: "workflow.yml", Content: source}},
		Options: analysis.Options{Analyzers: []analyzer.Registration{{Analyzer: stepNameAnalyzer{}}}},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(len(result.Diagnostics), "lint errors found by actionlint")
	// Output: 1 lint errors found by actionlint
}
