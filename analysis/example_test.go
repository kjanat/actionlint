package analysis_test

import (
	"context"
	"fmt"

	"actionlint.kjanat.dev/v2/analysis"
)

func ExampleAnalyze() {
	source := []byte(`on: push
jobs:
  build:
    runs-on: ubuntu-latest
    needs: deploy
    steps:
      - run: echo hello
`)
	result, err := analysis.Analyze(context.Background(), analysis.Request{
		Sources: []analysis.Source{{Path: "workflow.yml", Content: source}},
	})
	if err != nil {
		fmt.Println("incomplete:", err)
	}
	for _, finding := range result.Diagnostics {
		fmt.Printf("%s:%d:%d %s [%s]\n", finding.Path, finding.Start.Line, finding.Start.Column, finding.Message, finding.Rule)
	}
	// Output:
	// workflow.yml:3:3 job "build" needs job "deploy" which does not exist in this workflow [job-needs]
}
