package suppression

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

func TestInlineSuppressionFlowScalarBoundary(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"\"first\n    body_marker\"", "'first\n    body_marker'", "first\n    body_marker"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			source := "# actionlint:ignore-next-line expression\n- {run: " + value + ", shell: bash}\n- run: sibling\n"
			directives, invalid := ParseInlineSuppressions([]byte(source), []string{"expression"}, rule.Names{})
			if len(invalid) != 0 {
				t.Fatal(invalid)
			}
			result, _ := Evaluate(directives, nil, rule.Names{})
			if !result.Suppresses("expression", sourcetext.LineRange{First: 3, Last: 3}) || result.Suppresses("expression", sourcetext.LineRange{First: 4, Last: 4}) {
				t.Fatalf("flow scalar suppression must end before sibling: %+v", directives)
			}
		})
	}
}

func TestInlineSuppressionMultilineScalarBoundary(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"first\n  body_marker",
		"'first\n  body_marker'",
		"\"first\n  body_marker\"",
		"\"first \\\"quoted\\\"\n  body_marker\"",
		"\"first \\\n  body_marker\n  \"",
		"'first ''quoted''\n  body_marker\n  '",
		"!!str &script\n  first\n  body_marker",
		"&script !!str\n  'first\n  body_marker'",
		"!!str\n  &script\n  \"first\n  body_marker\"",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(value+ending, func(t *testing.T) {
				t.Parallel()
				input := "# actionlint:ignore-next-line expression\nrun: " + value + "\nnext: sibling\n"
				body, sibling := 0, 0
				for i, line := range strings.Split(input, "\n") {
					if strings.Contains(line, "body_marker") {
						body = i + 1
					}
					if strings.Contains(line, "next:") {
						sibling = i + 1
					}
				}
				input = strings.ReplaceAll(input, "\n", ending)
				directives, invalid := ParseInlineSuppressions([]byte(input), []string{"expression"}, rule.Names{})
				if len(invalid) != 0 {
					t.Fatal(invalid)
				}
				result, _ := Evaluate(directives, nil, rule.Names{})
				if !result.Suppresses("expression", sourcetext.LineRange{First: body, Last: body}) || result.Suppresses("expression", sourcetext.LineRange{First: sibling, Last: sibling}) {
					t.Fatalf("scalar body should be ignored without swallowing sibling: %+v", directives)
				}
				if len(directives) != 1 || directives[0].Lines.First != 2 || directives[0].Lines.Last != sibling-1 {
					t.Fatalf("wrong complete scalar extent: %+v", directives)
				}
			})
		}
	}
}
