package checks

import (
	"testing"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
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
			rule.checkTimezone(&workflowlang.String{Value: tz.name, Pos: &workflowlang.Pos{Line: 1, Col: 1}})
			if valid := len(rule.out) == 0; valid != tz.valid {
				t.Fatalf("valid=%v, expected %v: %v", valid, tz.valid, rule.out)
			}
		})
	}
}
