package plugins_test

import (
	"context"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/development/tests/conformance/wasm/dependencypins"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func Example_dependencyFacts() {
	result, err := checking.Analyze(context.Background(),

		checking.Options{Analyzers: []inputs.Registration{{Checker: dependencypins.Checker{}}}}, []workspace.InputFile{{Path: "workflow.yml", Content: []byte(`on: push
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
