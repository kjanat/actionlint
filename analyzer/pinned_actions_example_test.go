package analyzer_test

import (
	"context"
	"fmt"

	"actionlint.kjanat.dev/analysis"
	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/examples/plugins/pinnedactions"
)

func ExampleAnalyzer_dependencies() {
	result, err := analysis.Analyze(context.Background(), analysis.Request{
		Sources: []analysis.Source{{Path: "workflow.yml", Content: []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
`)}},
		Options: analysis.Options{Analyzers: []analyzer.Registration{{Analyzer: pinnedactions.Analyzer{}}}},
	})
	if err != nil {
		panic(err)
	}
	for _, finding := range result.Diagnostics {
		if finding.Rule == "pinned-actions" {
			fmt.Println(finding.Message)
		}
	}
	// Output: action must be pinned to a full commit hash
}
