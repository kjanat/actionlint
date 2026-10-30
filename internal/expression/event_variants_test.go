package expression

import "testing"

func TestEventGuardNarrowing(t *testing.T) {
	opened := NewStrictObjectType(map[string]Type{"action": StringType(), "title": StringType()})
	labeled := NewStrictObjectType(map[string]Type{"action": StringType(), "label": StringType()})
	push := NewStrictObjectType(map[string]Type{"commits": NewArrayType(StringType()).Type()})
	event := NewEventType([]EventVariant{
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
			checker := NewChecker(false, nil)
			checker.UpdateEvent(event)
			checker.SetContextAvailability([]string{"github", "env"})
			node, err := NewParser().Parse(NewLexer(tc.source + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			_, findings := checker.Check(node)
			if len(findings) != tc.errors {
				t.Fatalf("want %d errors; got %v", tc.errors, findings)
			}
			// A previous branch must not narrow the caller's next expression.
			node, err = NewParser().Parse(NewLexer("github.event.label}}"))
			if err != nil {
				t.Fatal(err)
			}
			if _, findings := checker.Check(node); len(findings) != 0 {
				t.Fatal(findings)
			}
			if len(event.EventVariants()) != 3 {
				t.Fatal("shared alternatives were mutated")
			}
		})
	}
}

func TestEventGuardEliminatedBranches(t *testing.T) {
	push := NewStrictObjectType(map[string]Type{"commits": NewArrayType(StringType()).Type()})
	event := NewEventType([]EventVariant{{Event: "push", Payload: push}})
	for _, source := range []string{
		"github.event_name == 'issues' && github.event.issue.title",
		"github.event_name == format('iss{0}', 'ues') && github.event.issue.title",
		"github.event_name != 'push' && github.event.issue.title",
		"github.event_name == 'push' || github.event.issue.title",
		"case(github.event_name == 'issues', github.event.issue.title, '')",
		"case(github.event_name == 'push', '', github.event.issue.title)",
	} {
		t.Run(source, func(t *testing.T) {
			checker := NewChecker(false, nil)
			checker.UpdateEvent(event)
			checker.SetContextAvailability([]string{"github"})
			node, err := NewParser().Parse(NewLexer(source + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			if _, findings := checker.Check(node); len(findings) != 0 {
				t.Fatalf("unreachable event branch reported: %v", findings)
			}
			node, err = NewParser().Parse(NewLexer("github.event.issue.title}}"))
			if err != nil {
				t.Fatal(err)
			}
			if _, findings := checker.Check(node); len(findings) != 1 {
				t.Fatalf("guard leaked to next expression: %v", findings)
			}
			if len(event.EventVariants()) != 1 {
				t.Fatal("shared event variants mutated")
			}
		})
	}
}
