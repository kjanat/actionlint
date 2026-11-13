package typecheck

import (
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func TestEventGuardNarrowing(t *testing.T) {
	t.Parallel()
	opened := exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"action": exprtypes.StringType(), "title": exprtypes.StringType()})
	labeled := exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"action": exprtypes.StringType(), "label": exprtypes.StringType()})
	push := exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"commits": exprtypes.NewArrayType(exprtypes.StringType()).Type()})
	event := exprtypes.NewEventType([]exprtypes.EventVariant{
		{Event: "issues", Action: "opened", Payload: opened},
		{Event: "issues", Action: "labeled", Payload: labeled},
		{Event: "push", Payload: push},
	})
	for _, tc := range []struct {
		source string
		errors int
	}{
		{"github.event.label", 0},
		{"github.event_name == 'push' && github.event.label", 1},
		{"github.event_name != 'issues' && github.event.label", 1},
		{"github.event_name == format('pu{0}', 'sh') && github.event.label", 1},
		{"format('pu{0}', 'sh') == github.event_name && github.event.label", 1},
		{"github.event_name == fromJSON('\"push\"') && github.event.label", 1},
		{"github.event.action == format('open{0}', 'ed') && github.event.label", 1},
		{"format('label{0}', 'ed') == github.event.action && github.event.label", 0},

		{"github.event_name == 'issues' || github.event.label", 1},
		{"github.event.action != '0' && github.event.commits", 1},
		{"github.event.action == '0' && github.event.label", 1},
		{"'0' == github.event.action && github.event.label", 1},
		{"!(github.event_name == 'issues') && github.event.label", 1},
		{"'PUSH' == github['EVENT_NAME'] && github.event.commits", 0},
		{"github.event_name == 'issues' && github.event.action == 'opened' && github.event.label", 1},
		{"github.event_name == 'issues' && github.event.action == 'labeled' && github.event.label", 0},
		{"github.event_name == 'issues' && (github.event.action != 'opened' || github.event.label)", 1},
		{"(github.event_name == 'push' || github.event.action == 'labeled') && github.event.label", 0},
		{"(github.event_name == 'issues' && github.event.action == 'opened') && github.event.label", 1},
		{"(github.event_name == 'issues' && github.event.action == 'opened' && github.event.title) || github.event.label", 0},
		// Unknown predicates cannot discard a possible event.
		{"env.DYNAMIC && github.event.label", 0},
		{"case(github.event_name == 'push', github.event.label, '')", 1},
		{"case(github.event_name == 'issues', github.event.label, '')", 0},
		{"case(github.event_name == 'issues', '', github.event.label)", 1},
		{"case(github.event_name == 'push', '', github.event.action == 'opened', github.event.label, '')", 1},
		{"case(github.event_name == 'push', '', github.event.action == 'opened', '', github.event.title)", 1},
		{"case(github.event_name == 'push', '', github.event.action == 'opened', github.event.title, github.event.label)", 0},
		{"case(github.event_name == 'issues', case(github.event.action == 'opened', github.event.label, ''), '')", 1},
		{"case(env.DYNAMIC == 'yes', github.event.label, '')", 0},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
			contextspec.SetEventPayloadType(checker, event)
			checker.SetContextAvailability([]string{"github", "env"})
			node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(tc.source + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			_, findings := checker.Check(expreval.EvaluateSubexpressions(node))
			if len(findings) != tc.errors {
				t.Fatalf("want %d errors; got %v", tc.errors, findings)
			}
			// A previous branch must not narrow the caller's next expression.
			node, err = exprsyntax.NewParser().Parse(exprsyntax.NewLexer("github.event.label}}"))
			if err != nil {
				t.Fatal(err)
			}
			if _, findings := checker.Check(expreval.EvaluateSubexpressions(node)); len(findings) != 0 {
				t.Fatal(findings)
			}
			if len(event.EventVariants()) != 3 {
				t.Fatal("shared alternatives were mutated")
			}
		})
	}
}

func TestEventGuardEliminatedBranches(t *testing.T) {
	t.Parallel()
	push := exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"commits": exprtypes.NewArrayType(exprtypes.StringType()).Type()})
	event := exprtypes.NewEventType([]exprtypes.EventVariant{{Event: "push", Payload: push}})
	for _, source := range []string{
		"github.event_name == 'issues' && github.event.issue.title",
		"github.event_name == format('iss{0}', 'ues') && github.event.issue.title",
		"github.event_name != 'push' && github.event.issue.title",
		"github.event_name == 'push' || github.event.issue.title",
		"case(github.event_name == 'issues', github.event.issue.title, '')",
		"case(github.event_name == 'push', '', github.event.issue.title)",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
			contextspec.SetEventPayloadType(checker, event)
			checker.SetContextAvailability([]string{"github"})
			node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(source + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			if _, findings := checker.Check(expreval.EvaluateSubexpressions(node)); len(findings) != 0 {
				t.Fatalf("unreachable event branch reported: %v", findings)
			}
			node, err = exprsyntax.NewParser().Parse(exprsyntax.NewLexer("github.event.issue.title}}"))
			if err != nil {
				t.Fatal(err)
			}
			if _, findings := checker.Check(expreval.EvaluateSubexpressions(node)); len(findings) != 1 {
				t.Fatalf("guard leaked to next expression: %v", findings)
			}
			if len(event.EventVariants()) != 1 {
				t.Fatal("shared event variants mutated")
			}
		})
	}
}
