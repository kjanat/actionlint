package local

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/execution"
)

func TestActionEntryHasNoCallerBranchJoin(t *testing.T) {
	resolver := NewResolver(nil, nil)
	resolver.checkoutEnvUnknown, resolver.shellEnvUnknown, resolver.persistentEnvUnknown = true, true, true
	resolver.checkoutShell = execution.Shell{Name: "bash"}
	leave := resolver.EnterComposite(execution.ActionEntry())
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
