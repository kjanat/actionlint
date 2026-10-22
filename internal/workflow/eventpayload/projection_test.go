package eventpayload

import (
	"strings"
	"testing"

	expr "actionlint.kjanat.dev/internal/expression"
)

func TestPayloadExpressions(t *testing.T) {
	for _, tc := range []struct {
		name         string
		triggers     []Trigger
		source, want string
	}{
		{"PR title", []Trigger{{Name: "pull_request"}}, "github.event.pull_request.title", ""},
		{"PR typo", []Trigger{{Name: "pull_request"}}, "github.event.pull_request.titel", `did you mean "title"`},
		{"bracket typo", []Trigger{{Name: "pull_request"}}, "github['event']['pull_request']['titel']", `did you mean "title"`},
		{"bracket casing", []Trigger{{Name: "pull_request"}}, "github['EVENT']['PULL_REQUEST']['TITLE']", ""},
		{"array", []Trigger{{Name: "push"}}, "github.event.commits[0].message", ""},
		{"array typo", []Trigger{{Name: "push"}}, "github.event.commits[0].mesage", `property "mesage"`},
		{"filter", []Trigger{{Name: "push"}}, "github.event.commits.*.message", ""},
		{"union PR", []Trigger{{Name: "push"}, {Name: "pull_request"}}, "github.event.pull_request.title", ""},
		{"union push", []Trigger{{Name: "push"}, {Name: "pull_request"}}, "github.event.commits[0].message", ""},
		{"target", []Trigger{{Name: "pull_request_target"}}, "github.event.pull_request.title", ""},
		{"free payload", []Trigger{{Name: "repository_dispatch"}}, "github.event.client_payload.whatever.nested", ""},
		{"unknown", []Trigger{{Name: "new_event"}}, "github.event.future.field", ""},
		{"unknown action", []Trigger{{Name: "issues", Actions: []string{"future"}}}, "github.event.future.field", ""},
		{"caller", []Trigger{{Name: "workflow_call"}}, "github.event.future.field", ""},
		{"action subset", []Trigger{{Name: "issues", Actions: []string{"labeled"}}}, "github.event.label.name", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := expr.NewChecker(false, nil)
			c.UpdateEvent(FromTriggers(tc.triggers))
			c.SetContextAvailability([]string{"github"})
			n, err := expr.NewParser().Parse(expr.NewLexer(tc.source + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			_, errors := c.Check(n)
			if tc.want == "" {
				if len(errors) != 0 {
					t.Fatalf("unexpected: %v", errors)
				}
			} else if len(errors) != 1 || !strings.Contains(errors[0].Message, tc.want) {
				t.Fatalf("want %q, got %v", tc.want, errors)
			}
		})
	}
}
