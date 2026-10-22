package builtin_test

import (
	"fmt"
	"strings"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestWorkflowCallCacheModeLiteralExpressions(t *testing.T) {
	for _, kind := range []workflowlang.CacheModeKind{workflowlang.CacheModeRead, workflowlang.CacheModeWrite, workflowlang.CacheModeWriteOnly, workflowlang.CacheModeNone} {
		for _, fromAST := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/AST=%v", kind, fromAST), func(t *testing.T) {
				caller := `on: push
cache-mode: ${{ 'none' }}
jobs:
  call:
    cache-mode: ${{ '` + kind.String() + `' }}
    uses: $/callee.yaml
`
				callee := `on: workflow_call
cache-mode: ${{ 'write' }}
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
				diagnostics := checkCacheModeCall(t, caller, map[string]string{"callee.yaml": callee}, fromAST)
				if kind == workflowlang.CacheModeWrite {
					if len(diagnostics) != 0 {
						t.Fatal(diagnostics)
					}
					return
				}
				if len(diagnostics) != 1 || diagnostics[0].Rule != "workflow-call" || diagnostics[0].Start.Line != 6 || !strings.Contains(diagnostics[0].Message, `requests cache-mode "write" but the calling job allows "`+kind.String()+`"`) {
					t.Fatalf("folded cache-mode did not retain its ceiling: %v", diagnostics)
				}
			})
		}
	}
}
