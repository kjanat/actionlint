package parser_test

import (
	"testing"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

func TestCheckStringRejectsRawTagReview(t *testing.T) {
	for _, allowEmpty := range []bool{false, true} {
		p := &workflowparser.Parser{}
		n := &syntax.Node{Kind: syntax.ScalarNode, Tag: "!custom", Value: "max", Style: syntax.TaggedStyle, Line: 1, Column: 1}
		if p.CheckString(n, allowEmpty) {
			t.Fatal("invalid tag accepted")
		}
	}
}

func TestWorkflowExpressionLiteralBoundaryReview(t *testing.T) {
	for _, value := range []string{"pre-${{ '' }}", "${{ '' }}-post", "${{ '' }}-${{ '' }}", "${{ '' }}tail}}", "  ${{ '' }}  ", "${{ '' }}\n", "\t${{ '' }}"} {
		if got, known := workflowlang.WorkflowExpressionLiteral(&workflowlang.String{Value: value}); known {
			t.Errorf("fragment %q folded to %#v", value, got)
		}
	}
	if got, known := workflowlang.WorkflowExpressionLiteral(&workflowlang.String{Value: "${{  ''  }}"}); !known || got != "" {
		t.Fatalf("sole expression: %#v, known=%v", got, known)
	}
}
