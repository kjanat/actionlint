package suppression

import (
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"

	"github.com/google/go-cmp/cmp"
)

func TestInlineSuppressionStages(t *testing.T) {
	const source = `name: one
  two # actionlint:ignore cache-operation, cache-operation -- reviewed -- intentionally unused
run-name: |
  two # actionlint:ignore cache-operation, cache-operation -- reviewed -- intentionally unused
`
	comments := collectInlineSuppressionComments([]byte(source))
	if len(comments) != 1 || comments[0].pos != (diagnostic.Position{Line: 2, Column: 7}) || comments[0].standalone {
		t.Fatalf("want only the real closing comment at 2:7, got %+v", comments)
	}
	directive, err := parseInlineSuppression(comments[0], []string{"cache-operation"})
	if err != nil {
		t.Fatal(err)
	}
	want := Directive{
		Position: diagnostic.Position{Line: 2, Column: 7}, First: 2, Last: 2,
		Rules: []string{"cache-operation"}, Reason: "reviewed -- intentionally unused",
	}
	if diff := cmp.Diff(want, directive, cmp.AllowUnexported(Directive{})); diff != "" {
		t.Fatal(diff)
	}
}
