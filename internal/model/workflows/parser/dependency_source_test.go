package parser

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestDependencySpansRequirePhysicalIndex(t *testing.T) {
	t.Parallel()
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: owner/action@v1\n")
	workflow, findings := ParseWorkflow(content)
	if len(findings) != 0 {
		t.Fatal(findings)
	}
	action, present := workflow.Jobs["test"].Steps[0].Exec.(*definition.ExecAction)
	if !present {
		t.Fatal("fixture action unavailable")
	}
	action.Uses.SourceScalar.End = sourcetext.Position{}
	parser := NewSourceParser(content)
	parser.IndexDependencySpans(workflow)
	if got := action.Uses.SourceScalar.End; got != (sourcetext.Position{}) {
		t.Fatalf("unindexed dependency gained a span: %v", got)
	}
	root, err := codec.ParseYAML(content)
	if err != nil {
		t.Fatal(err)
	}
	parser.IndexScalarSources(root)
	parser.IndexDependencySpans(workflow)
	if got := action.Uses.SourceScalar.End; got != (sourcetext.Position{Line: 6, Column: 30}) {
		t.Fatalf("indexed dependency end = %v; want 6:30", got)
	}
}
