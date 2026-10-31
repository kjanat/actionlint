package local

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
)

func TestRelativeRepositoryRootUsesSessionWorkingDirectory(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repository")
	writeFixture(t, root, "source/action.yml", "name: relative\ndescription: session root\nruns: {using: composite, steps: []}\n")
	store, err := capture.New(t.Context(), nil, func(_ context.Context, path string) ([]byte, error) {
		return os.ReadFile(filepath.FromSlash(path))
	})
	if err != nil {
		t.Fatal(err)
	}
	capturefixture.CloseCaptures(t, store)
	cache := NewCache(t.Context(), "repository", nil)
	var observed capture.Snapshot
	cache.Configure(Options{Captures: store, Context: t.Context(), WorkingDir: base, OnDocument: func(captured capture.Snapshot, _ string, _ *actionmeta.Metadata, _ error) { observed = captured }})
	resolver := NewResolver(cache, map[string]string{"./generated": "./source"})
	resolver.caseInsensitive = true
	result, _ := resolver.LookupMetadata("./generated")
	if result.Metadata() == nil || result.Err() != nil {
		t.Fatalf("relative source mapping used process cwd: %+v error=%v", result, result.Err())
	}
	document, ok := resolver.SourceDocument(result)
	if !ok || document.Capture().Revision() != observed.Revision() || document.Address() != filepath.Join(root, "source", "action.yml") {
		t.Fatal("observer and resolver did not carry the same capture")
	}
	if err := store.Verify(observed, document.Content()); err != nil {
		t.Fatal(err)
	}
	if got, want := result.Metadata().Path(), filepath.Join("repository", "source", "action.yml"); got != want {
		t.Fatalf("display path = %q, want %q", got, want)
	}
	if _, state, err := store.Lookup(filepath.Join(root, "source", "action.yml")); err != nil || state != capture.Captured {
		t.Fatalf("absolute read address missing: state=%v error=%v", state, err)
	}
}
