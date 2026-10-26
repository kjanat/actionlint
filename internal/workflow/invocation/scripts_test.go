package invocation_test

import (
	"errors"
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/source"
)

func TestScriptBudgetRetainsFirstBoundaryAndStopsCalls(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprintf("nested=%t", nested), func(t *testing.T) {
			budget := invocation.DefaultBudget()
			budget.MaxScripts = 1
			b := builder(t, invocation.EntryWorkflow, budget)
			owner, document := b.RootID(), "entry"
			if nested {
				call := begin(t, b, owner, reference(document, "call"))
				expand(t, b, call, reference("action", "document"))
				owner, document = call.ID(), "action"
			}
			first, err := addScript(t, b, owner, reference(document, "first"), invocation.ScriptRun, "")
			if err != nil {
				t.Fatal(err)
			}
			boundary := reference(document, "second")
			if id, err := addScript(t, b, owner, boundary, invocation.ScriptActionInput, "script"); id != "" || !errors.Is(err, invocation.ErrScriptBudget) {
				t.Fatalf("exhaustion allocated a script or lost its cause: %q %v", id, err)
			}
			if _, err := addScript(t, b, owner, reference(document, "third"), invocation.ScriptRun, ""); !errors.Is(err, invocation.ErrScriptBudget) {
				t.Fatalf("lost script stop cause: %v", err)
			}
			if _, err := addScript(t, b, owner, reference("stale", "third"), invocation.ScriptRun, ""); !errors.Is(err, invocation.ErrInvalidSource) {
				t.Fatalf("stop concealed invalid source: %v", err)
			}
			if ticket := begin(t, b, owner, reference(document, "later-call")); ticket.Status() != invocation.Stopped || ticket.ID() != "" || !errors.Is(ticket.Err(), invocation.ErrScriptBudget) {
				t.Fatal("call admission lost script exhaustion")
			}
			if nested {
				closeCall(t, b, owner)
			}
			g := publish(t, b)
			stop, exists := g.Stop()
			if !exists || stop.Parent != owner || stop.Site != boundary || stop.Limit != invocation.LimitScripts || stop.Max != 1 || !g.Incomplete() {
				t.Fatalf("lost first script boundary: %+v", stop)
			}
			if scripts := g.Scripts(); len(scripts) != 1 || scripts[0].ID != first {
				t.Fatalf("script missing: %+v", scripts)
			}
			if _, interrupted := g.EntryInterruption(); interrupted {
				t.Fatal("budget exhaustion became an operational interruption")
			}
		})
	}
}

func TestScriptBudgetIsSharedAcrossOwners(t *testing.T) {
	budget := invocation.DefaultBudget()
	budget.MaxScripts = 2
	b := builder(t, invocation.EntryWorkflow, budget)
	if _, err := addScript(t, b, b.RootID(), reference("entry", "run"), invocation.ScriptRun, ""); err != nil {
		t.Fatal(err)
	}
	first := begin(t, b, b.RootID(), reference("entry", "first-call"))
	expand(t, b, first, reference("action", "document"))
	if _, err := addScript(t, b, first.ID(), reference("action", "run"), invocation.ScriptRun, ""); err != nil {
		t.Fatal(err)
	}
	closeCall(t, b, first.ID())
	second := begin(t, b, b.RootID(), reference("entry", "second-call"))
	expand(t, b, second, reference("action", "document"))
	if _, err := addScript(t, b, second.ID(), reference("action", "run"), invocation.ScriptRun, ""); !errors.Is(err, invocation.ErrScriptBudget) {
		t.Fatalf("new owner reset script budget: %v", err)
	}
	closeCall(t, b, second.ID())
	g := publish(t, b)
	if len(g.Scripts()) != 2 || !g.Incomplete() {
		t.Fatal("shared quota lost prior owners' scripts")
	}
}

func TestInvalidScriptAttemptsDoNotConsumeBudget(t *testing.T) {
	budget := invocation.DefaultBudget()
	budget.MaxScripts = 2
	b := builder(t, invocation.EntryWorkflow, budget)
	first := reference("entry", "first")
	if _, err := addScript(t, b, b.RootID(), first, invocation.ScriptRun, ""); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []struct {
		owner invocation.ID
		ref   source.Reference
		want  error
	}{
		{b.RootID(), first, invocation.ErrDuplicateScript},
		{b.RootID(), reference("wrong", "run"), invocation.ErrInvalidSource},
		{"invalid", first, invocation.ErrForeignContext},
	} {
		owner := invocation.Context{}
		if attempt.owner == b.RootID() {
			owner = b.RootContext()
		}
		if _, err := b.AddScript(owner, attempt.ref, invocation.ScriptRun, ""); !errors.Is(err, attempt.want) {
			t.Fatalf("invalid attempt: %v, want %v", err, attempt.want)
		}
	}
	if _, err := addScript(t, b, b.RootID(), reference("entry", "second"), invocation.ScriptRun, ""); err != nil {
		t.Fatal(err)
	}
	g := publish(t, b)
	if _, stopped := g.Stop(); stopped || g.Incomplete() || len(g.Scripts()) != 2 {
		t.Fatal("exactly-at-limit or invalid attempts incorrectly exhausted budget")
	}
}

func TestCallBudgetRemainsCauseOfRejectedScript(t *testing.T) {
	budget := invocation.DefaultBudget()
	budget.MaxCalls = 1
	b := builder(t, invocation.EntryWorkflow, budget)
	opaque(t, b, begin(t, b, b.RootID(), reference("entry", "call")))
	ticket := begin(t, b, b.RootID(), reference("entry", "boundary"))
	if _, err := addScript(t, b, b.RootID(), reference("entry", "script"), invocation.ScriptRun, ""); !errors.Is(err, invocation.ErrCallBudget) || !errors.Is(err, ticket.Err()) {
		t.Fatalf("script replaced call-budget stop: %v", err)
	}
	g := publish(t, b)
	stop, exists := g.Stop()
	if !exists || stop.Site != reference("entry", "boundary") || stop.Limit != invocation.LimitTotal || len(g.Scripts()) != 0 {
		t.Fatalf("changed first stop: %+v", stop)
	}
}

func TestInterruptedScriptOwnerRetainsRegisteredScripts(t *testing.T) {
	b := builder(t, invocation.EntryWorkflow, invocation.DefaultBudget())
	call := begin(t, b, b.RootID(), reference("entry", "call"))
	expand(t, b, call, reference("action", "document"))
	id, err := addScript(t, b, call.ID(), reference("action", "run"), invocation.ScriptRun, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Interrupt(invocation.ReasonCanceled, "cancelled"); err != nil {
		t.Fatal(err)
	}
	g := publish(t, b)
	script, exists := g.Script(id)
	owner, hasOwner := g.Call(script.Owner)
	if !exists || !hasOwner || owner.ID() != call.ID() || owner.Outcome.Kind() != invocation.OutcomeInterrupted || !g.Incomplete() {
		t.Fatal("interruption lost the script's owning call")
	}
}

func TestScriptBudgetMustBePositive(t *testing.T) {
	for _, limit := range []int{0, -1} {
		budget := invocation.DefaultBudget()
		budget.MaxScripts = limit
		if _, err := invocation.NewBuilder(invocation.EntryWorkflow, reference("entry", "root"), budget); !errors.Is(err, invocation.ErrInvalidBudget) {
			t.Fatalf("accepted script budget %d: %v", limit, err)
		}
	}
}

func TestBudgetStopClassificationPreservesOtherCauses(t *testing.T) {
	budget := invocation.DefaultBudget()
	budget.MaxScripts = 1
	b := builder(t, invocation.EntryWorkflow, budget)
	if _, err := addScript(t, b, b.RootID(), reference("entry", "first"), invocation.ScriptRun, ""); err != nil {
		t.Fatal(err)
	}
	_, stop := addScript(t, b, b.RootID(), reference("entry", "second"), invocation.ScriptRun, "")
	if !errors.Is(stop, invocation.ErrScriptBudget) {
		t.Fatal(stop)
	}
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{"direct", stop, true},
		{"wrapped", fmt.Errorf("operation: %w", stop), true},
		{"joined stop", errors.Join(stop, stop), true},
		{"joined failure", errors.Join(stop, errors.New("failure")), false},
		{"wrapped joined failure", fmt.Errorf("operation: %w", errors.Join(stop, errors.New("failure"))), false},
		{"sentinel without boundary", invocation.ErrScriptBudget, false},
		{"nil", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := b.IsBudgetStop(test.err); got != test.want {
				t.Fatalf("IsBudgetStop = %t, want %t", got, test.want)
			}
		})
	}
	other := builder(t, invocation.EntryWorkflow, budget)
	if other.IsBudgetStop(stop) {
		t.Fatal("builder without an exhausted budget accepted another builder's stop")
	}
}

func TestRegisteredScriptsFollowTheirOwningBodies(t *testing.T) {
	entry := source.Reference{Document: "workflow.yml", Revision: "workflow-revision", Node: "root"}
	declaration := entry
	declaration.Node = "steps[0]"
	callSite := entry
	callSite.Node = "steps[1]"
	child := source.Reference{Document: "action.yml", Revision: "action-revision", Node: "root"}
	childDeclaration := child
	childDeclaration.Node = "steps[0]"
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, entry, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	rootScript, err := addScript(t, builder, builder.RootID(), declaration, invocation.ScriptRun, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := addScript(t, builder, builder.RootID(), declaration, invocation.ScriptRun, ""); !errors.Is(err, invocation.ErrDuplicateScript) {
		t.Fatalf("duplicate traversal: %v", err)
	}
	ticket, err := builder.Begin(builder.RootContext(), callSite, invocation.Facts{Uses: "$/action", HasUses: true, Condition: invocation.ConditionEnabled})
	if err != nil {
		t.Fatal(err)
	}
	ticketContext, admitted := ticket.Context()
	if !admitted {
		t.Fatal("call ticket was not admitted")
	}
	if _, err := builder.AddScript(ticketContext, childDeclaration, invocation.ScriptRun, ""); !errors.Is(err, invocation.ErrParentNotOpen) {
		t.Fatalf("pending call admitted a script: %v", err)
	}
	outcome, err := invocation.Expanded(child)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Resolve(ticketContext, outcome); err != nil {
		t.Fatal(err)
	}
	if _, err := addScript(t, builder, builder.RootID(), declaration, invocation.ScriptRun, ""); !errors.Is(err, invocation.ErrTraversal) {
		t.Fatalf("ancestor body admitted a script while its child was current: %v", err)
	}
	if _, err := builder.AddScript(ticketContext, declaration, invocation.ScriptRun, ""); !errors.Is(err, invocation.ErrInvalidSource) {
		t.Fatalf("call admitted another document's declaration: %v", err)
	}
	wrongRevision := childDeclaration
	wrongRevision.Revision = "stale-revision"
	if _, err := builder.AddScript(ticketContext, wrongRevision, invocation.ScriptRun, ""); !errors.Is(err, invocation.ErrInvalidSource) {
		t.Fatalf("call admitted a stale declaration: %v", err)
	}
	childScript, err := builder.AddScript(ticketContext, childDeclaration, invocation.ScriptActionInput, "script")
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.Close(ticketContext); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.AddScript(ticketContext, childDeclaration, invocation.ScriptRun, ""); !errors.Is(err, invocation.ErrParentNotOpen) {
		t.Fatalf("closed call admitted a script: %v", err)
	}
	graph, err := builder.Publish()
	if err != nil {
		t.Fatal(err)
	}
	scripts := graph.Scripts()
	if len(scripts) != 2 || scripts[0].ID != rootScript || scripts[1].ID != childScript || scripts[1].Owner != ticket.ID() || scripts[1].Declaration != childDeclaration {
		t.Fatalf("script registrations lost their owner/source: %+v", scripts)
	}
	scripts[1].Owner = builder.RootID()
	registered, exists := graph.Script(childScript)
	if !exists || registered.Owner != ticket.ID() {
		t.Fatal("mutating a returned script slice changed the graph")
	}
	if _, exists := graph.Script(ticket.ID()); exists {
		t.Fatal("call ID was accepted as a script occurrence")
	}
	if _, err := addScript(t, builder, builder.RootID(), declaration, invocation.ScriptActionInput, "another"); !errors.Is(err, invocation.ErrPublished) {
		t.Fatalf("published builder admitted a script: %v", err)
	}
}

func TestInterruptedBuilderRejectsFurtherScripts(t *testing.T) {
	entry := source.Reference{Document: "workflow.yml", Revision: "revision", Node: "root"}
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, entry, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	script, err := addScript(t, builder, builder.RootID(), entry, invocation.ScriptRun, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.InterruptEntry(invocation.ReasonCanceled, "cancelled after the first script"); err != nil {
		t.Fatal(err)
	}
	if _, err := addScript(t, builder, builder.RootID(), entry, invocation.ScriptRun, ""); !errors.Is(err, invocation.ErrInterrupted) {
		t.Fatalf("interrupted builder admitted a script: %v", err)
	}
	graph, err := builder.Publish()
	if err != nil {
		t.Fatal(err)
	}
	if _, present := graph.Script(script); !present || !graph.Incomplete() {
		t.Fatal("interruption lost previously registered script facts")
	}
}
