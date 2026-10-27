package local

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/source"
)

func compositeContext(t *testing.T, facts invocation.Facts) invocation.Context {
	t.Helper()
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, source.Reference{Document: "fixture", Node: "entry"}, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := builder.Begin(builder.RootContext(), source.Reference{Document: "fixture", Node: "call"}, facts)
	if err != nil {
		t.Fatal(err)
	}
	context, admitted := ticket.Context()
	if !admitted {
		t.Fatal("fixture context was not admitted")
	}
	return context
}

func TestCompositeForkIsolatesTraversalAndJoinsConditionalEffects(t *testing.T) {
	for _, tc := range []struct {
		name                                             string
		condition                                        invocation.Condition
		continueOnError, background, samePath, unchanged bool
		wantKind                                         execution.DirectoryKind
	}{
		{name: "enabled", condition: invocation.ConditionEnabled, wantKind: execution.DirectoryKnown},
		{name: "skipped", condition: invocation.ConditionSkipped, wantKind: execution.DirectoryKnown},
		{name: "runtime", condition: invocation.ConditionRuntimeDependent, wantKind: execution.DirectoryUnknown},
		{name: "continue on error", condition: invocation.ConditionEnabled, continueOnError: true, wantKind: execution.DirectoryUnknown},
		{name: "background", condition: invocation.ConditionEnabled, background: true, wantKind: execution.DirectoryUnknown},
		{name: "runtime same placement", condition: invocation.ConditionRuntimeDependent, samePath: true, wantKind: execution.DirectoryKnown},
		{name: "continue same placement", condition: invocation.ConditionEnabled, continueOnError: true, samePath: true, wantKind: execution.DirectoryUnknown},
		{name: "background unchanged", condition: invocation.ConditionEnabled, background: true, unchanged: true, wantKind: execution.DirectoryKnown},
		{name: "runtime unchanged", condition: invocation.ConditionRuntimeDependent, unchanged: true, wantKind: execution.DirectoryKnown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := NewResolver(nil, nil)
			parent.platform, parent.caseInsensitive = runnerlang.Windows, true
			parent.checkoutShell = execution.Shell{Name: "bash", Implicit: true}
			parent.setCheckout(execution.Directory{Kind: execution.DirectoryKnown, Path: "."})
			before := parent.state()
			context := compositeContext(t, invocation.Facts{
				Condition: tc.condition, ContinueOnError: tc.continueOnError, Background: tc.background,
				CheckoutEnvironmentUnknown: true, LookupEnvironmentUnknown: true,
			})
			child, err := parent.ForkComposite(context)
			if err != nil {
				t.Fatal(err)
			}
			if child == parent || child.platform != parent.platform || child.caseInsensitive != parent.caseInsensitive || child.checkoutShell != (execution.Shell{}) || !child.checkoutEnvUnknown || !child.shellEnvUnknown {
				t.Fatal("child did not inherit isolated invocation assumptions")
			}
			if child.CheckoutState() != before.checkout {
				t.Fatal("fork lost the immutable checkout chain")
			}
			child.persistentEnvUnknown = true
			child.platform, child.caseInsensitive = runnerlang.MacOrLinux, false
			child.checkoutShell = execution.Shell{Name: "pwsh"}
			if !tc.unchanged {
				path := "child"
				if tc.samePath {
					path = "."
				}
				child.restoreCheckout(execution.NewCheckoutPlacement(execution.Checkout{Directory: execution.Directory{Kind: execution.DirectoryKnown, Path: path}}, child.CheckoutState()))
			}
			if parent.state() != before {
				t.Fatal("active child mutated its parent")
			}
			if err := parent.JoinComposite(child); err != nil {
				t.Fatal(err)
			}
			if parent.platform != before.platform || parent.caseInsensitive != before.caseInsensitive || parent.checkoutShell != before.checkoutShell || parent.checkoutEnvUnknown != before.checkoutEnvUnknown || parent.shellEnvUnknown != before.shellEnvUnknown {
				t.Fatal("child-local shell/environment/runner assumptions leaked")
			}
			if parent.persistentEnvUnknown != (tc.condition != invocation.ConditionSkipped) {
				t.Fatal("persistent environment effect joined incorrectly")
			}
			placement := parent.CheckoutState()
			wantPath := "child"
			if tc.condition == invocation.ConditionSkipped || tc.samePath || tc.unchanged {
				wantPath = "."
			}
			if !placement.Present() || placement.Details().Directory.Kind != tc.wantKind || placement.Details().Directory.Path != wantPath {
				t.Fatalf("joined checkout=%+v, want kind=%v path=%q", placement, tc.wantKind, wantPath)
			}
			if (tc.condition == invocation.ConditionSkipped || tc.unchanged) && placement != before.checkout {
				t.Fatal("discarded or absent checkout effects changed chain identity")
			}
		})
	}
}

func TestCompositeForkLifecycleRejectsForeignOverlappingAndStaleJoins(t *testing.T) {
	context := compositeContext(t, invocation.Facts{Condition: invocation.ConditionEnabled})
	parent := NewResolver(nil, nil)
	if child, err := parent.ForkComposite(invocation.Context{}); child != nil || !errors.Is(err, ErrInvalidCompositeContext) {
		t.Fatalf("invalid context fork: %v %v", child, err)
	}
	child, err := parent.ForkComposite(context)
	if err != nil {
		t.Fatal(err)
	}
	if sibling, err := parent.ForkComposite(context); sibling != nil || !errors.Is(err, ErrCompositeScopeActive) {
		t.Fatalf("overlapping fork: %v %v", sibling, err)
	}
	foreign := NewResolver(nil, nil)
	if err := foreign.JoinComposite(child); !errors.Is(err, ErrForeignCompositeScope) {
		t.Fatalf("foreign parent join: %v", err)
	}
	grandchild, err := child.ForkComposite(context)
	if err != nil {
		t.Fatal(err)
	}
	grandchild.setCheckout(execution.Directory{Kind: execution.DirectoryKnown, Path: "grandchild"})
	grandchild.persistentEnvUnknown = true
	if err := parent.JoinComposite(child); !errors.Is(err, ErrCompositeScopeActive) {
		t.Fatalf("unfinished descendant join: %v", err)
	}
	if err := child.JoinComposite(grandchild); err != nil {
		t.Fatal(err)
	}
	if parent.CheckoutState().Present() || parent.persistentEnvUnknown {
		t.Fatal("nested join bypassed the immediate parent")
	}
	if err := parent.JoinComposite(child); err != nil {
		t.Fatal(err)
	}
	if placement := parent.CheckoutState(); !placement.Present() || placement.Details().Directory.Path != "grandchild" || !parent.persistentEnvUnknown {
		t.Fatalf("nested effects lost: checkout=%+v persistent=%t", placement, parent.persistentEnvUnknown)
	}
	if err := parent.JoinComposite(child); !errors.Is(err, ErrCompositeScopeJoined) {
		t.Fatalf("repeated join: %v", err)
	}
	if _, err := child.ForkComposite(context); !errors.Is(err, ErrCompositeScopeJoined) {
		t.Fatalf("retired child fork: %v", err)
	}
	stale, err := parent.ForkComposite(context)
	if err != nil {
		t.Fatal(err)
	}
	stale.setCheckout(execution.Directory{Kind: execution.DirectoryKnown, Path: "stale-child"})
	parent.setCheckout(execution.Directory{Kind: execution.DirectoryKnown, Path: "new-parent"})
	if err := parent.JoinComposite(stale); !errors.Is(err, ErrCompositeParentChanged) {
		t.Fatalf("stale parent join: %v", err)
	}
	if placement := parent.CheckoutState(); placement.Details().Directory.Path != "new-parent" {
		t.Fatal("stale join overwrote newer parent state")
	}
	next, err := parent.ForkComposite(context)
	if err != nil {
		t.Fatal("failed join did not retire its scope:", err)
	}
	if err := parent.JoinComposite(next); err != nil {
		t.Fatal(err)
	}
}

func TestCompositeCheckoutHandlesDetachInputsDetailsAndLocalReplacements(t *testing.T) {
	parent := NewResolver(nil, nil)
	want := execution.Checkout{Directory: execution.Directory{Kind: execution.DirectoryKnown, Path: "parent"}, CaseInsensitive: true}
	input := want
	original := execution.NewCheckoutPlacement(input, execution.CheckoutPlacement{})
	parent.restoreCheckout(original)
	child, err := parent.ForkComposite(compositeContext(t, invocation.Facts{Condition: invocation.ConditionEnabled}))
	if err != nil {
		t.Fatal(err)
	}
	localHandle := child.CheckoutState()
	if parent.CheckoutState() != original || localHandle != original {
		t.Fatal("fork did not preserve immutable handle identity")
	}
	input.Directory.Path, input.Directory.Kind = "changed-input", execution.DirectoryUnknown
	input.CaseInsensitive, input.Foreign, input.Redirected = false, true, true
	details := child.CheckoutState().Details()
	details.Directory.Path, details.Directory.Kind = "changed-details", execution.DirectoryUnknown
	details.CaseInsensitive, details.Foreign, details.Redirected = false, true, true
	localHandle = execution.CheckoutPlacement{}
	if localHandle.Present() || parent.CheckoutState().Details() != want || child.CheckoutState().Details() != want || parent.CheckoutState().Details() == input || child.CheckoutState().Details() == details {
		t.Fatal("mutable input/details or local handle replacement changed shared checkout state")
	}
	childInput := execution.Checkout{Directory: execution.Directory{Kind: execution.DirectoryKnown, Path: "child"}}
	child.restoreCheckout(execution.NewCheckoutPlacement(childInput, child.CheckoutState()))
	childInput.Directory.Path = "changed-child-input"
	previous := child.CheckoutState().Previous()
	previousDetails := previous.Details()
	previousDetails.Directory.Path = "changed-previous-details"
	if previous != original || parent.CheckoutState() != original || previous.Details() != want || child.CheckoutState().Details().Directory.Path != "child" || child.CheckoutState().Details() == childInput || previous.Details() == previousDetails {
		t.Fatal("child traversal or detached previous-link details changed the parent")
	}
	if err := parent.JoinComposite(child); err != nil {
		t.Fatal(err)
	}
	if parent.CheckoutState().Details().Directory.Path != "child" || parent.CheckoutState().Previous() != original {
		t.Fatal("immutable checkout effects were not joined")
	}
}

func TestCompositeForkAdmitsOnlyOneConcurrentChild(t *testing.T) {
	parent := NewResolver(nil, nil)
	context := compositeContext(t, invocation.Facts{Condition: invocation.ConditionEnabled})
	type outcome struct {
		child *Resolver
		err   error
	}
	const count = 16
	returned := make(chan outcome, count)
	var wait sync.WaitGroup
	for range count {
		wait.Go(func() {
			child, err := parent.ForkComposite(context)
			returned <- outcome{child, err}
		})
	}
	wait.Wait()
	close(returned)
	var admitted *Resolver
	for result := range returned {
		if result.child != nil {
			if admitted != nil || result.err != nil {
				t.Fatal("more than one concurrent child admitted")
			}
			admitted = result.child
		} else if !errors.Is(result.err, ErrCompositeScopeActive) {
			t.Fatalf("concurrent fork failed for wrong reason: %v", result.err)
		}
	}
	if admitted == nil {
		t.Fatal("no child admitted")
	}
	if err := parent.JoinComposite(admitted); err != nil {
		t.Fatal(err)
	}
}

func TestCompositeEffectsJoinOnTraversalErrorWithoutReplacingCause(t *testing.T) {
	for _, stale := range []bool{false, true} {
		parent := NewResolver(nil, nil)
		cause := errors.New("composite traversal failed")
		err := func() (resultErr error) {
			child, err := parent.ForkComposite(compositeContext(t, invocation.Facts{Condition: invocation.ConditionEnabled}))
			if err != nil {
				return err
			}
			defer func() { resultErr = errors.Join(resultErr, parent.JoinComposite(child)) }()
			child.setCheckout(execution.Directory{Kind: execution.DirectoryKnown, Path: "child"})
			child.persistentEnvUnknown = true
			if stale {
				parent.setCheckout(execution.Directory{Kind: execution.DirectoryKnown, Path: "new-parent"})
			}
			return cause
		}()
		if !errors.Is(err, cause) || errors.Is(err, ErrCompositeParentChanged) != stale {
			t.Fatalf("traversal/join causes not preserved: %v", err)
		}
		wantPath := "child"
		if stale {
			wantPath = "new-parent"
		}
		if placement := parent.CheckoutState(); !placement.Present() || placement.Details().Directory.Path != wantPath || parent.persistentEnvUnknown == stale {
			t.Fatalf("failed traversal effects: checkout=%+v persistent=%t", placement, parent.persistentEnvUnknown)
		}
	}
}

func TestCompositeForksShareCapturedSourcesEvenWhenEffectsAreSkipped(t *testing.T) {
	root := t.TempDir()
	parentPath := writeFixture(t, root, "parent/action.yml", "name: parent\ndescription: parent\nruns: {using: composite, steps: []}\n")
	childPath := writeFixture(t, root, "child/action.yml", "name: child\ndescription: child\nruns: {using: composite, steps: []}\n")
	badPath := writeFixture(t, root, "bad/action.yml", "name: [\n")
	cache := newCapturedActionCache(t, root, nil)
	mappings := map[string]string{"./alias": "./child"}
	parent := NewResolver(cache, mappings)
	mappings["./alias"] = "./bad"
	child, err := parent.ForkComposite(compositeContext(t, invocation.Facts{Condition: invocation.ConditionSkipped}))
	if err != nil {
		t.Fatal(err)
	}
	grandchild, err := child.ForkComposite(compositeContext(t, invocation.Facts{Condition: invocation.ConditionEnabled}))
	if err != nil {
		t.Fatal(err)
	}
	if parent.documents != child.documents || child.documents != grandchild.documents || parent.base != child.base || child.base != grandchild.base {
		t.Fatal("fork did not retain its shared metadata/source owner")
	}
	var wait sync.WaitGroup
	for _, lookup := range []struct {
		resolver *Resolver
		spec     string
	}{{parent, "$/parent"}, {child, "$/alias"}, {grandchild, "$/bad"}} {
		wait.Go(func() {
			for range 8 {
				lookup.resolver.LookupMetadata(lookup.spec)
				parent.Sources()
				child.SourceDocuments()
			}
		})
	}
	wait.Wait()
	for _, resolver := range []*Resolver{parent, child, grandchild} {
		documents := resolver.SourceDocuments()
		if len(documents) != 3 {
			t.Fatalf("fork lost consulted sources: %v", documents)
		}
		for _, path := range []string{parentPath, childPath, badPath} {
			document, ok := documents[path]
			if !ok || document.Path() != path || document.Address() != filepath.Clean(path) || !document.Capture().Valid() {
				t.Fatalf("source provenance lost for %q: %+v", path, document)
			}
		}
		delete(documents, childPath)
		if len(resolver.SourceDocuments()) != 3 {
			t.Fatal("mutating source projection changed the shared owner")
		}
	}
	bad, cached := parent.LookupMetadata("$/bad")
	if !cached || bad.Status() != MetadataParseFailed || bad.Err() == nil {
		t.Fatalf("fork parse failure was not shared through cache: status=%v cached=%t error=%v", bad.Status(), cached, bad.Err())
	}
	if document, ok := parent.SourceDocument(bad); !ok || document.Path() != badPath || document.Document().ActionParseError() == nil {
		t.Fatal("failed metadata lost its captured parse record")
	}
	separate := NewResolver(cache, nil)
	if len(separate.SourceDocuments()) != 0 {
		t.Fatal("separate workflow inherited another resolver's used sources")
	}
	if _, cached := separate.LookupMetadata("$/child"); !cached || len(separate.SourceDocuments()) != 1 || len(parent.SourceDocuments()) != 3 {
		t.Fatal("cache hits lost sources or shared them across separate workflow owners")
	}
	grandchild.InvalidateCheckout()
	grandchild.persistentEnvUnknown = true
	if err := child.JoinComposite(grandchild); err != nil {
		t.Fatal(err)
	}
	if err := parent.JoinComposite(child); err != nil {
		t.Fatal(err)
	}
	if parent.CheckoutState().Present() || parent.persistentEnvUnknown || len(parent.SourceDocuments()) != 3 {
		t.Fatal("skipped child leaked effects or lost consulted source evidence")
	}
}
