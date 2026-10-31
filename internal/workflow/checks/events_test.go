package checks

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/declaration"

	"actionlint.kjanat.dev/v2/internal/webhook"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestSchemaNestedTimezoneAliases(t *testing.T) {
	// LoadLocation can resolve casing variants through the host filesystem.
	// Invalid/Timezone exercises rejection independently of filesystem casing.
	for _, tz := range []struct {
		name  string
		valid bool
	}{{"UTC", true}, {"Etc/UTC", true}, {"Universal", true}, {"Europe/Amsterdam", true}, {"Local", false}, {"local", false}, {"Invalid/Timezone", false}} {
		t.Run(tz.name, func(t *testing.T) {
			rule := &eventsCheck{name: "events"}
			rule.checkTimezone(ownedString(&workflowlang.String{Value: tz.name, Pos: &workflowlang.Pos{Line: 1, Col: 1}}))
			if valid := len(rule.out) == 0; valid != tz.valid {
				t.Fatalf("valid=%v, expected %v: %v", valid, tz.valid, rule.out)
			}
		})
	}
}

func TestWebhookOnlyActivitiesRejected(t *testing.T) {
	for _, tc := range []struct {
		event, activity string
		valid           bool
	}{
		{"merge_group", "checks_requested", true},
		{"merge_group", "destroyed", false},
		{"check_suite", "requested", false},
		{"issue_comment", "pinned", false},
		{"pull_request_target", "milestoned", true},
		{"pull_request_target", "demilestoned", true},
		{"pull_request_target", "stacked", true},
		{"pull_request", "stacked", true},
		{"image_version", "created", true},
		{"image_version", "ready", true},
		{"image_version", "deleted", true},
	} {
		t.Run(tc.event+"/"+tc.activity, func(t *testing.T) {
			rule := &eventsCheck{name: "events"}
			pos := &workflowlang.Pos{Line: 1, Col: 1}
			rule.checkTypes(tc.event, pos, slices.Values([]declaration.StringView{ownedString(&workflowlang.String{Value: tc.activity, Pos: pos})}), webhook.AllWebhookTypes[tc.event])
			if valid := len(rule.out) == 0; valid != tc.valid {
				t.Fatalf("valid=%v, want %v: %v", valid, tc.valid, rule.out)
			}
		})
	}
}

func ownedString(value *workflowlang.String) declaration.StringView {
	return declaration.Publish("", &workflowlang.Workflow{Name: value}, false, "").Bind("test").Workflow().Name()
}
