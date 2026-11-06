package inputs_test

import (
	"context"
	"fmt"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	workspace "actionlint.kjanat.dev/v2/internal/input/workspace"
	workflowmodel "actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
	"actionlint.kjanat.dev/v2/internal/results"
)

// stepNameAnalyzer checks a document without depending on the traversal host.
type stepNameAnalyzer struct{}

func (stepNameAnalyzer) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "step-name", Description: "Checks every step has a name", Category: "conventions", RequiredInputs: []rulemeta.InputKind{rulemeta.Source}}
}

func (stepNameAnalyzer) CheckDocument(_ context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	if input.Kind != "workflow" {
		return nil, nil
	}
	workflow, _ := workflowparser.Parse([]byte(input.Content))
	if workflow == nil {
		return nil, nil
	}
	var findings []results.Diagnostic
	var checkSteps func([]*workflowmodel.Step)
	checkSteps = func(steps []*workflowmodel.Step) {
		for _, step := range steps {
			if step.Name == nil {
				findings = append(findings, results.Diagnostic{
					Message: "every step must have its name",
					Start:   sourcepos.Position{Line: step.Pos.Line, Column: step.Pos.Col},
				})
			}
			if parallel, ok := step.Exec.(*workflowmodel.ExecParallel); ok {
				checkSteps(parallel.Steps)
			}
		}
	}
	for _, job := range workflow.Jobs {
		checkSteps(job.Steps)
	}
	return findings, nil
}

func Example_stepNames() {
	source := []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`)
	result, err := checking.Analyze(context.Background(),

		checking.Options{Analyzers: []checkinputs.Registration{{Checker: stepNameAnalyzer{}}}}, []workspace.Source{{Path: "workflow.yml", Content: source}},
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(len(result.Diagnostics), "lint errors found by actionlint")
	// Output: 1 lint errors found by actionlint
}
