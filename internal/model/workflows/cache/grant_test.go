package cache

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/calls"
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
	if EffectiveMode(workflowMode, nil) != workflowMode || EffectiveMode(workflowMode, jobMode) != jobMode {
		t.Fatal("inheritance did not select the declaring mode")
	}
	if grant := FromDeclaration(jobMode); !grant.Present() || grant.Kind() != workflowlang.CacheModeNone {
		t.Fatalf("grant=%+v", grant)
	}
}
