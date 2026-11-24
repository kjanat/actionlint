package correctness

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/contexts/events"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestSchemaNestedTimezoneAliases(t *testing.T) {
	t.Parallel()
	// LoadLocation can resolve casing variants through the host filesystem.
	// Invalid/Timezone exercises rejection independently of filesystem casing.
	for _, tz := range []struct {
		name  string
		valid bool
	}{{"UTC", true}, {"Etc/UTC", true}, {"Universal", true}, {"Europe/Amsterdam", true}, {"Local", false}, {"local", false}, {"Invalid/Timezone", false}} {
		t.Run(tz.name, func(t *testing.T) {
			t.Parallel()
			rule := &eventsCheck{Rule: "events"}
			rule.checkTimezone(stringFixture(&definition.String{Value: tz.name, Pos: &sourcetext.Position{Line: 1, Column: 1}}))
			if valid := len(rule.Findings) == 0; valid != tz.valid {
				t.Fatalf("valid=%v, expected %v: %v", valid, tz.valid, rule.Findings)
			}
		})
	}
}

func TestWebhookOnlyActivitiesRejected(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			rule := &eventsCheck{Rule: "events"}
			pos := &sourcetext.Position{Line: 1, Column: 1}
			rule.checkTypes(tc.event, pos, []*definition.String{{Value: tc.activity, Pos: pos}}, events.AllWebhookTypes[tc.event])
			if valid := len(rule.Findings) == 0; valid != tc.valid {
				t.Fatalf("valid=%v, want %v: %v", valid, tc.valid, rule.Findings)
			}
		})
	}
}

func stringFixture(value *definition.String) *definition.String {
	return value
}
