package local

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/source"
)

func TestActionEntryHasNoCallerBranchJoin(t *testing.T) {
	resolver := NewResolver(nil, nil)
	resolver.checkoutEnvUnknown, resolver.shellEnvUnknown, resolver.persistentEnvUnknown = true, true, true
	resolver.checkoutShell = execution.Shell{Name: "bash"}
	builder, err := invocation.NewBuilder(invocation.EntryAction, source.Reference{Document: "fixture", Node: "entry"}, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	child, err := resolver.ForkComposite(builder.RootContext())
	if err != nil {
		t.Fatal(err)
	}
	if child.checkoutShell.Name != "" {
		t.Fatal("entry inherited the caller checkout shell")
	}
	child.setCheckout(execution.Directory{Kind: execution.DirectoryKnown, Path: "child"})
	if resolver.CheckoutState().Present() || resolver.checkoutShell.Name != "bash" {
		t.Fatal("child traversal changed caller state before joining")
	}
	if err := resolver.JoinComposite(child); err != nil {
		t.Fatal(err)
	}
	if resolver.checkoutShell.Name != "bash" || !resolver.checkoutEnvUnknown || !resolver.shellEnvUnknown || !resolver.persistentEnvUnknown {
		t.Fatal("entry did not restore local environment assumptions")
	}
	if state := resolver.CheckoutState(); !state.Present() || state.Details().Directory.Kind != execution.DirectoryKnown || state.Details().Directory.Path != "child" {
		t.Fatalf("entry performed a caller branch join: %+v", state)
	}
}
