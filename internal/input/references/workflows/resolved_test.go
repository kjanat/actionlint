package workflows

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
	"actionlint.kjanat.dev/v2/internal/model/calls"
)

func TestResolvedWorkflowLookupNeverLoadsAndDetachesMetadata(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	reads := map[string]int{}
	store, err := capture.New(t.Context(), nil, func(_ context.Context, path string) ([]byte, error) {
		name := filepath.Base(path)
		reads[name]++
		switch name {
		case "outer.yml":
			return []byte("on: workflow_call\njobs: {nested: {uses: ./inner.yml}}\n"), nil
		case "inner.yml":
			return []byte("on: workflow_call\njobs: {cycle: {uses: $/outer.yml}}\n"), nil
		default:
			return nil, os.ErrNotExist
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	capturefixture.CloseCaptures(t, store)
	cache := NewCache(root, nil)
	cache.SetSources(t.Context(), store, root)
	resolved := cache.Resolved()
	if metadata, err := resolved.LookupForCall("./outer.yml"); metadata != nil || err != nil || len(reads) != 0 {
		t.Fatalf("a lookup started acquisition: present=%t reads=%v error=%v", metadata != nil, reads, err)
	}
	if err := cache.LoadCalls(t.Context(), []string{"./outer.yml", "$/outer.yml"}); err != nil {
		t.Fatal(err)
	}
	metadata, err := resolved.LookupForCall("$/outer.yml")
	if err != nil || metadata == nil || metadata.JobCacheAccess["nested"].Uses != "./inner.yml" {
		t.Fatalf("prepared definition is unavailable: metadata=%v error=%v", metadata, err)
	}
	metadata.JobCacheAccess["nested"] = calls.CacheAccess{}
	again, err := resolved.LookupForCall("./outer.yml")
	if err != nil || again == nil || again.JobCacheAccess["nested"].Uses != "./inner.yml" {
		t.Fatal("a lookup consumer changed the cached definition")
	}
	if metadata, err := resolved.LookupForCall("./missing.yml"); metadata != nil || err != nil {
		t.Fatalf("unknown metadata produced a definition: present=%t error=%v", metadata != nil, err)
	}
	if len(reads) != 2 || reads["outer.yml"] != 1 || reads["inner.yml"] != 1 {
		t.Fatalf("repeated references, cycles or lookups caused extra reads: %v", reads)
	}
}

func TestInvalidWorkflowMetadataRejectsQuarantinedCapture(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := capture.New(t.Context(), nil, func(context.Context, string) ([]byte, error) {
		return []byte("on: [broken\n"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	capturefixture.CloseCaptures(t, store)
	cache := NewCache(root, nil)
	cache.SetSources(t.Context(), store, root)
	if _, err := cache.LoadForCall(t.Context(), "./broken.yml"); err == nil {
		t.Fatal("broken metadata was accepted")
	}
	captured, err := store.Read(t.Context(), filepath.Join(root, "broken.yml"), source.YAML)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Quarantine(captured, "conflicting declaration kinds"); err == nil {
		t.Fatal("source quarantine did not report a conflict")
	}
	_, err = cache.Resolved().LookupForCall("./broken.yml")
	if _, quarantined := errors.AsType[capture.ConflictError](err); !quarantined {
		t.Fatalf("invalid metadata survived source quarantine: %v", err)
	}
}
