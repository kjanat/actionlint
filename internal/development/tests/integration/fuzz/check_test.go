package fuzz

import (
	"io/fs"
	"testing"

	workspace "actionlint.kjanat.dev/v2/internal/input/workspace"
	workflowmodel "actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/checking"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func parseWorkflowPanicFree(data []byte) *workflowmodel.Workflow {
	// Avoid Parse() panicking. It panics when go-yaml panics
	defer func() { _ = recover() }()
	w, _ := workflowparser.Parse(data)
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

		_, _ = checking.Analyze(t.Context(),

			checking.Options{ReadFile: func(string) ([]byte, error) { return nil, fs.ErrNotExist }}, []workspace.Source{{Path: "test.yaml", Content: data}},
		)
	})
}
