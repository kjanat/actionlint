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
	leave := resolver.EnterComposite(builder.RootContext())
	if resolver.checkoutShell.Name != "" {
		t.Fatal("entry inherited the caller checkout shell")
	}
	resolver.setCheckout(execution.Directory{Kind: execution.DirectoryKnown, Path: "child"})
	leave()
	if resolver.checkoutShell.Name != "bash" || !resolver.checkoutEnvUnknown || !resolver.shellEnvUnknown || !resolver.persistentEnvUnknown {
		t.Fatal("entry did not restore local environment assumptions")
	}
	if state := resolver.CheckoutState(); state == nil || state.Directory.Kind != execution.DirectoryKnown || state.Directory.Path != "child" {
		t.Fatalf("entry performed a caller branch join: %+v", state)
	}
}
