package workspace

import (
	"context"
	"errors"
	"os"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/files"
)

func TestPreparedActionsDoNotAcquireMetadata(t *testing.T) {
	t.Parallel()
	reads := 0
	cache := newCapturedActionCache(t, t.TempDir(), nil)
	cache.Configure(Options{CustomReader: true, Reader: files.New(t.Context(), func(string) ([]byte, error) {
		reads++
		return []byte("name: test\ndescription: test\nruns: {using: composite, steps: []}\n"), nil
	}, nil)})
	resolver := NewResolver(cache, nil)
	prepared := resolver.Prepared()
	if _, _, document, err := prepared.FindMetadataDocument("$/first"); err == nil || document.Capture().Valid() || reads != 0 {
		t.Fatalf("unprepared lookup acquired or exposed a source: reads=%d error=%v", reads, err)
	}
	if err := resolver.PrepareMetadata(t.Context(), "$/first"); err != nil {
		t.Fatal(err)
	}
	acquired := reads
	for range 2 {
		metadata, _, document, err := prepared.FindMetadataDocument("$/first")
		if err != nil || metadata == nil || !document.Capture().Valid() || reads != acquired {
			t.Fatalf("prepared lookup changed acquisition: present=%t reads=%d error=%v", metadata != nil, reads, err)
		}
	}
	if err := resolver.PrepareMetadata(t.Context(), "$/second"); err != nil {
		t.Fatal(err)
	}
	if _, _, document, err := prepared.FindMetadataDocument("$/first"); err == nil || document.Capture().Valid() {
		t.Fatal("lookup exposed another step's metadata source")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	acquired = reads
	if err := resolver.PrepareMetadata(ctx, "$/third"); !errors.Is(err, context.Canceled) || reads != acquired {
		t.Fatalf("canceled preparation acquired metadata: reads=%d error=%v", reads, err)
	}
}

func TestActionPreparationPreservesReadFailures(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{context.Canceled, os.ErrPermission} {
		t.Run(cause.Error(), func(t *testing.T) {
			t.Parallel()
			cache := newCapturedActionCache(t, t.TempDir(), nil)
			cache.Configure(Options{CustomReader: true, Reader: files.New(t.Context(), func(string) ([]byte, error) {
				return nil, cause
			}, nil)})
			resolver := NewResolver(cache, nil)
			if err := resolver.PrepareMetadata(t.Context(), "$/unreadable"); !errors.Is(err, cause) {
				t.Fatalf("read failure became prepared input: %v", err)
			}
		})
	}
}

func TestActionPreparationAccountsForReportedParseFailures(t *testing.T) {
	t.Parallel()
	cache := newCapturedActionCache(t, t.TempDir(), nil)
	cache.Configure(Options{CustomReader: true, Reader: files.New(t.Context(), func(string) ([]byte, error) {
		return []byte("name: [broken\n"), nil
	}, nil)})
	resolver := NewResolver(cache, nil)
	for _, report := range []bool{true, false} {
		if err := resolver.PrepareMetadata(t.Context(), "$/broken"); err != nil {
			t.Fatal(err)
		}
		prepared := resolver.Prepared()
		for range 2 {
			_, _, document, err := prepared.FindMetadataDocument("$/broken")
			if (err != nil) != report || !document.Capture().Valid() {
				t.Fatalf("wrong prepared diagnostic: report=%t error=%v", report, err)
			}
		}
		result, _ := prepared.LookupMetadata("$/broken")
		if result.Status() != MetadataParseFailed || result.Err() == nil {
			t.Fatal("reporting consumed the acquisition failure")
		}
	}
}
