package cache

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/calls"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestGrantInheritanceDistinguishesMissingInvalidAndNone(t *testing.T) {
	t.Parallel()
	parent := FromMetadata(calls.DeclaredCacheMode(workflowlang.CacheModeWrite))
	for _, tc := range []struct {
		name     string
		metadata calls.CacheModeDeclaration
		kind     workflowlang.CacheModeKind
		bits     uint8
		known    bool
	}{
		{"missing inherits", calls.CacheModeDeclaration{}, workflowlang.CacheModeWrite, 3, true},
		{"invalid overrides", calls.DeclaredCacheMode(workflowlang.CacheModeInvalid), workflowlang.CacheModeInvalid, 0, false},
		{"none overrides", calls.DeclaredCacheMode(workflowlang.CacheModeNone), workflowlang.CacheModeNone, 0, true},
		{"read overrides", calls.DeclaredCacheMode(workflowlang.CacheModeRead), workflowlang.CacheModeRead, 1, true},
		{"write-only overrides", calls.DeclaredCacheMode(workflowlang.CacheModeWriteOnly), workflowlang.CacheModeWriteOnly, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			child := FromMetadata(tc.metadata)
			grant := parent.Override(child)
			bits, known := grant.Capabilities()
			if !grant.Present() || grant.Kind() != tc.kind || bits != tc.bits || known != tc.known {
				t.Fatalf("grant=%+v capabilities=%d known=%t", grant, bits, known)
			}
			if parent.Kind() != workflowlang.CacheModeWrite {
				t.Fatal("child changed its parent")
			}
		})
	}
	missing := FromMetadata(calls.CacheModeDeclaration{})
	if _, known := missing.Capabilities(); missing.Present() || known {
		t.Fatal("missing mode became an explicit restriction")
	}
}

func TestEffectiveModePreservesPublishedDeclaration(t *testing.T) {
	t.Parallel()
	workflowMode := &workflowlang.CacheMode{Kind: workflowlang.CacheModeRead, Pos: &workflowlang.Pos{Line: 2, Col: 13}}
	jobMode := &workflowlang.CacheMode{Kind: workflowlang.CacheModeNone, Pos: &workflowlang.Pos{Line: 5, Col: 17}}
	decoded := &workflowlang.Workflow{CacheMode: workflowMode, Jobs: map[string]*workflowlang.Job{
		"inherited": {}, "explicit": {CacheMode: jobMode},
	}}
	workflow := declaration.Publish("", decoded, false, "").Bind("test").Workflow()
	inherited, _ := workflow.Job("inherited")
	explicit, _ := workflow.Job("explicit")
	selected := EffectiveMode(workflow.CacheMode(), explicit.CacheMode())
	grant := FromDeclaration(selected)
	workflowMode.Kind, jobMode.Kind = workflowlang.CacheModeWrite, workflowlang.CacheModeWrite
	if !EffectiveMode(workflow.CacheMode(), inherited.CacheMode()).SameDeclaration(workflow.CacheMode()) || !selected.SameDeclaration(explicit.CacheMode()) || grant.Kind() != workflowlang.CacheModeNone {
		t.Fatal("inheritance changed the source declaration or published mode")
	}
	if pos, ok := selected.Pos(); !ok || pos.Line != 5 || pos.Col != 17 {
		t.Fatalf("selected position=%v present=%t", pos, ok)
	}
}
