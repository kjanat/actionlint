package parser

import (
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
)

func TestDependencySpansRequirePhysicalIndex(t *testing.T) {
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: owner/action@v1\n")
	workflow, findings := Parse(content)
	if len(findings) != 0 {
		t.Fatal(findings)
	}
	action, present := workflow.Jobs["test"].Steps[0].Exec.(*workflowlang.ExecAction)
	if !present {
		t.Fatal("fixture action unavailable")
	}
	action.Uses.SourceScalar.End = workflowlang.Pos{}
	parser := NewSourceParser(content)
	parser.IndexDependencySpans(workflow)
	if got := action.Uses.SourceScalar.End; got != (workflowlang.Pos{}) {
		t.Fatalf("unindexed dependency gained a span: %v", got)
	}
	root, err := codec.Parse(content)
	if err != nil {
		t.Fatal(err)
	}
	parser.IndexScalarSources(root)
	parser.IndexDependencySpans(workflow)
	if got := action.Uses.SourceScalar.End; got != (workflowlang.Pos{Line: 6, Col: 30}) {
		t.Fatalf("indexed dependency end = %v; want 6:30", got)
	}
}
