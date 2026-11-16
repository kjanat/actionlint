package cache

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestGrantInheritanceDistinguishesMissingInvalidAndNone(t *testing.T) {
	t.Parallel()
	parent := FromMetadata(calls.DeclaredCacheMode(definition.CacheModeWrite))
	for _, tc := range []struct {
		name     string
		metadata calls.CacheModeDeclaration
		kind     definition.CacheModeKind
		bits     uint8
		known    bool
	}{
		{"missing inherits", calls.CacheModeDeclaration{}, definition.CacheModeWrite, 3, true},
		{"invalid overrides", calls.DeclaredCacheMode(definition.CacheModeInvalid), definition.CacheModeInvalid, 0, false},
		{"none overrides", calls.DeclaredCacheMode(definition.CacheModeNone), definition.CacheModeNone, 0, true},
		{"read overrides", calls.DeclaredCacheMode(definition.CacheModeRead), definition.CacheModeRead, 1, true},
		{"write-only overrides", calls.DeclaredCacheMode(definition.CacheModeWriteOnly), definition.CacheModeWriteOnly, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			child := FromMetadata(tc.metadata)
			grant := parent.Override(child)
			bits, known := grant.Capabilities()
			if !grant.Present() || grant.Kind() != tc.kind || bits != tc.bits || known != tc.known {
				t.Fatalf("grant=%+v capabilities=%d known=%t", grant, bits, known)
			}
			if parent.Kind() != definition.CacheModeWrite {
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
	workflowMode := &definition.CacheMode{Kind: definition.CacheModeRead, Pos: &source.Position{Line: 2, Column: 13}}
	jobMode := &definition.CacheMode{Kind: definition.CacheModeNone, Pos: &source.Position{Line: 5, Column: 17}}
	if EffectiveMode(workflowMode, nil) != workflowMode || EffectiveMode(workflowMode, jobMode) != jobMode {
		t.Fatal("inheritance did not select the declaring mode")
	}
	if grant := FromDeclaration(jobMode); !grant.Present() || grant.Kind() != definition.CacheModeNone {
		t.Fatalf("grant=%+v", grant)
	}
}
