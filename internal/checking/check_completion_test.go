package checking

import (
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestIncompleteChecksPreserveRevisionRuleAndReason(t *testing.T) {
	t.Parallel()
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

func TestIncompleteChecksSeparateCallsEntriesAndRevisions(t *testing.T) {
	t.Parallel()
	ref := sourcepos.Reference{Document: "shared-action", Revision: "action-revision"}
	entry := sourcepos.Reference{Document: "workflow", Revision: "workflow-revision", Node: "root"}
	var completion checkCompletion
	rootID, err := invocation.NewRootID(invocation.EntryWorkflow, entry)
	if err != nil {
		t.Fatal(err)
	}
	var firstID invocation.ID
	for _, node := range []string{"first-call", "second-call"} {
		id, err := invocation.NewCallID(rootID, sourcepos.Reference{Document: entry.Document, Revision: entry.Revision, Node: node})
		if err != nil {
			t.Fatal(err)
		}
		if firstID == "" {
			firstID = id
		}
		completion.markUnavailableInvocation(ref, report.CheckInvocation{Entry: entry, ID: id}, "shellcheck", "tool unavailable")
	}
	otherEntry := entry
	otherEntry.Revision = "new-workflow-revision"
	completion.markUnavailableInvocation(ref, report.CheckInvocation{Entry: otherEntry, ID: firstID}, "shellcheck", "tool unavailable")
	otherEntry.Document = "other-workflow"
	otherID, err := invocation.NewRootID(invocation.EntryWorkflow, otherEntry)
	if err != nil {
		t.Fatal(err)
	}
	completion.markIncompleteInvocation(ref, report.CheckInvocation{Entry: otherEntry, ID: otherID})
	checks := completion.incompleteChecks()
	if len(checks) != 4 {
		t.Fatalf("calls or captured entry revisions were merged: %+v", checks)
	}
	checks[0].Invocation.Entry.Revision = "consumer mutation"
	if completion.incompleteChecks()[0].Invocation.Entry.Revision == "consumer mutation" {
		t.Fatal("result consumer changed the stored entry revision")
	}
}

func TestCheckInvocationIndexDoesNotBorrowAnotherSourceRevision(t *testing.T) {
	t.Parallel()
	ref := sourcepos.Reference{Document: "workflow", Revision: "revision-one", Node: "root"}
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, ref, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	graph, err := builder.Publish()
	if err != nil {
		t.Fatal(err)
	}
	index := indexCheckInvocations(graph)
	contexts := index.forSource(ref)
	if len(contexts) != 1 || contexts[0].ID != graph.RootID() || contexts[0].Entry != ref {
		t.Fatalf("captured entry identity lost: %+v", contexts)
	}
	ref.Revision = "revision-two"
	contexts = index.forSource(ref)
	if len(contexts) != 1 || contexts[0] != (report.CheckInvocation{}) {
		t.Fatalf("unidentified invocation borrowed another revision: %+v", contexts)
	}
	contexts = indexCheckInvocations(invocation.Graph{}).forSource(ref)
	if len(contexts) != 1 || contexts[0] != (report.CheckInvocation{}) {
		t.Fatalf("unpublished graph invented an invocation: %+v", contexts)
	}
}
