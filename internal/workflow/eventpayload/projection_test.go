package eventpayload

import (
	"strings"
	"testing"

	expr "actionlint.kjanat.dev/v2/internal/expression"
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
		{"custom dispatch known fields", []Trigger{{Name: "repository_dispatch", Actions: []string{"custom"}}}, "github.event.repository.full_name", ""},
		{"custom dispatch known typo", []Trigger{{Name: "repository_dispatch", Actions: []string{"custom"}}}, "github.event.repository.full_nam", `did you mean "full_name"`},
		{"actionless deployment", []Trigger{{Name: "deployment"}}, "github.event.deployment.environment", ""},
		{"actionless deployment typo", []Trigger{{Name: "deployment"}}, "github.event.deployment.environmen", `did you mean "environment"`},
		{"unknown", []Trigger{{Name: "new_event"}}, "github.event.future.field", ""},
		{"unknown action", []Trigger{{Name: "issues", Actions: []string{"future"}}}, "github.event.future.field", ""},
		{"caller", []Trigger{{Name: "workflow_call"}}, "github.event.future.field", ""},
		{"action subset", []Trigger{{Name: "issues", Actions: []string{"labeled"}}}, "github.event.label.name", ""},
		{"default PR excludes labels", []Trigger{{Name: "pull_request"}}, "github.event.label.name", `property "label"`},
		{"explicit PR labels", []Trigger{{Name: "pull_request", Actions: []string{"labeled"}}}, "github.event.label.name", ""},
		{"default target excludes labels", []Trigger{{Name: "pull_request_target"}}, "github.event.label.name", `property "label"`},
		{"action guard narrows", []Trigger{{Name: "issues", Actions: []string{"opened", "labeled"}}}, "github.event.action == 'opened' && github.event.label.name", `property "label"`},
		{"label guard retains", []Trigger{{Name: "issues", Actions: []string{"opened", "labeled"}}}, "github.event.action == 'labeled' && github.event.label.name", ""},
		{"action guard excludes push", []Trigger{{Name: "push"}, {Name: "issues"}}, "github.event.action == 'labeled' && github.event.commits[0].message", `property "commits"`},
		{"negated action guard excludes push", []Trigger{{Name: "push"}, {Name: "issues"}}, "github.event.action != 'labeled' || github.event.commits[0].message", `property "commits"`},
		{"missing action matches empty", []Trigger{{Name: "push"}, {Name: "issues"}}, "github.event.action == '' && github.event.issue.title", `property "issue"`},
		{"action guard retains push otherwise", []Trigger{{Name: "push"}, {Name: "issues"}}, "github.event.action != 'labeled' && github.event.commits[0].message", ""},
		{"caller action remains unknown", []Trigger{{Name: "workflow_call"}, {Name: "issues"}}, "github.event.action == 'labeled' && github.event.commits[0].message", ""},
		{"dispatch action remains unknown", []Trigger{{Name: "repository_dispatch"}, {Name: "issues"}}, "github.event.action == 'custom' && github.event.client_payload.value", ""},
		{"future event action remains unknown", []Trigger{{Name: "new_event"}, {Name: "issues"}}, "github.event.action == 'labeled' && github.event.future.field", ""},
		{"event guard narrows", []Trigger{{Name: "push"}, {Name: "pull_request"}}, "github.event_name == 'push' && github.event.pull_request.title", `property "pull_request"`},
		{"event guard retains", []Trigger{{Name: "push"}, {Name: "pull_request"}}, "github.event_name == 'pull_request' && github.event.pull_request.title", ""},
		{"narrowed workflow run overlay", []Trigger{{Name: "push"}, {Name: "workflow_run"}}, "github.event_name == 'workflow_run' && github.event.workflow_run.head_repository.homepage", ""},
		{"caller remains open under guard", []Trigger{{Name: "workflow_call"}, {Name: "push"}}, "github.event_name == 'issues' && github.event.issue.title", ""},
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

func TestDefaultVariants(t *testing.T) {
	for _, event := range []string{"pull_request", "pull_request_target"} {
		projected := FromTriggers([]Trigger{{Name: event}})
		if len(projected.EventVariants) != 3 {
			t.Fatalf("%s: want three default actions, got %d", event, len(projected.EventVariants))
		}
		for _, variant := range projected.EventVariants {
			if variant.Event != event {
				t.Errorf("lost trigger identity: %#v", variant)
			}
			switch variant.Action {
			case "opened", "synchronize", "reopened":
			default:
				t.Errorf("unexpected default activity %q", variant.Action)
			}
		}
	}
}
