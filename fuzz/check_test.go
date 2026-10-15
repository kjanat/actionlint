package actionlint_fuzz

import (
	"io/fs"
	"testing"

	"actionlint.kjanat.dev/analysis"
	actionlint "actionlint.kjanat.dev/internal/engine"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func parseWorkflowPanicFree(data []byte) *workflowlang.Workflow {
	// Avoid Parse() panicking. It panics when go-yaml panics
	defer func() { _ = recover() }()
	w, _ := actionlint.Parse(data)
	return w
}

func FuzzCheck(f *testing.F) {
	f.Add([]byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`))
	f.Add([]byte(`on: [push]
jobs:
  t:
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
`))

	f.Fuzz(func(t *testing.T, data []byte) {
		w := parseWorkflowPanicFree(data)
		if w == nil {
			return
		}

		_, _ = analysis.Analyze(t.Context(), analysis.Request{
			Sources: []analysis.Source{{Path: "test.yaml", Content: data}},
			Options: analysis.Options{ReadFile: func(string) ([]byte, error) { return nil, fs.ErrNotExist }},
		})
	})
}
