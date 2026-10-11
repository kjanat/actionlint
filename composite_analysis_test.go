package actionlint

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCompositeAnalysisCancellation(t *testing.T) {
	for _, actionRule := range []bool{false, true} {
		name := "metadata steps"
		if actionRule {
			name = "invocation after action validation"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			outer := writeShellcheckFixture(t, root, "outer/action.yml", `name: outer
description: test
runs:
  using: composite
  steps:
    - uses: $/inner
`)
			inner := writeShellcheckFixture(t, root, "inner/action.yml", `name: inner
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: echo ok
`)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			outerReads, innerReads := 0, 0
			result, err := Analyze(ctx, AnalysisRequest{
				WorkingDir: root,
				Sources: []SourceUnit{{
					Path: "workflow.yml", Project: &Project{root: root},
					Content: []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: $/outer
`),
				}},
				OnRulesCreated: func(rules []Rule) []Rule {
					if actionRule {
						for _, rule := range rules {
							if _, ok := rule.(*RuleAction); ok {
								return []Rule{rule}
							}
						}
					}
					return nil
				},
				ReadFile: func(path string) ([]byte, error) {
					switch filepath.Clean(path) {
					case outer:
						outerReads++
						cancel()
					case inner:
						innerReads++
					}
					return os.ReadFile(path)
				},
			})
			if result == nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("wanted cancelled analysis, got result=%+v, error=%v", result, err)
			}
			if !slices.Contains(result.Inputs, outer) || slices.Contains(result.Inputs, inner) {
				t.Fatalf("partial result does not match consumed inputs: %v", result.Inputs)
			}
			if outerReads != 1 || innerReads != 0 {
				t.Fatalf("analysis continued after cancellation: outer reads=%d, inner reads=%d", outerReads, innerReads)
			}
		})
	}
}
