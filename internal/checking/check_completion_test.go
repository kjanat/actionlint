package checking

import (
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
)

func TestIncompleteChecksPreserveRevisionRuleAndReason(t *testing.T) {
	var usage checkCompletion
	usage.markUnavailable(sourcepos.Reference{Document: "z-action", Revision: "revision-one"}, "shellcheck", "tool unavailable")
	usage.markUnavailable(sourcepos.Reference{Document: "a-workflow", Revision: "revision-two"}, "workflow-run-names", "producer names unavailable")
	usage.markIncomplete(sourcepos.Reference{Document: "a-workflow", Revision: "revision-one"})
	checks := usage.incompleteChecks()
	if len(checks) != 3 || checks[0].Source.Document != "a-workflow" || checks[0].Source.Revision != "revision-one" || checks[0].Rule != "" || checks[0].Reason == "" {
		t.Fatalf("source-wide interruption or ordering lost: %+v", checks)
	}
	if checks[1].Source.Revision != "revision-two" || checks[1].Rule != "workflow-run-names" || checks[1].Reason != "producer names unavailable" || checks[2].Rule != "shellcheck" {
		t.Fatalf("check-specific completion records crossed revisions: %+v", checks)
	}
	checks[1].Reason = "consumer mutation"
	if usage.incompleteChecks()[1].Reason != "producer names unavailable" {
		t.Fatal("result consumer mutated session completion records")
	}
}
