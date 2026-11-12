package plugins_test

import (
	"context"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	dependencypins "actionlint.kjanat.dev/v2/internal/development/tests/conformance/wasm/dependency-pins"
	workspace "actionlint.kjanat.dev/v2/internal/input/workspace"
)

func Example_dependencyFacts() {
	result, err := checking.Analyze(context.Background(),

		checking.Options{Analyzers: []checkinputs.Registration{{Checker: dependencypins.Checker{}}}}, []workspace.Source{{Path: "workflow.yml", Content: []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
`)}},
	)
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
