package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
)

func TestMetadataAdmissionFailureNeverPublishesUsableMetadata(t *testing.T) {
	var source strings.Builder
	source.WriteString("name: rejected\ninputs: invalid\nruns:\n  using: composite\n  steps:\n    - run: echo unsafe\n      shell: bash\n      a0: &a0 [value]\n")
	for level := 1; level <= 8; level++ {
		fmt.Fprintf(&source, "      a%d: &a%d [*a%d, *a%d, *a%d]\n", level, level, level-1, level-1, level-1)
	}
	cache := newCapturedActionCache(t, t.TempDir(), nil)
	cache.Configure(Options{CustomReader: true, Reader: fsread.New(t.Context(), func(string) ([]byte, error) {
		return []byte(source.String()), nil
	}, nil)})
	for _, wantCached := range []bool{false, true} {
		result, cached := cache.LookupMetadata("./rejected")
		if cached != wantCached || result.Status() != MetadataParseFailed || result.Metadata() != nil || result.Err() == nil {
			t.Fatalf("rejected metadata became usable: status=%v metadata=%v cached=%v error=%v", result.Status(), result.Metadata(), cached, result.Err())
		}
		if !strings.Contains(result.Err().Error(), "inputs must be mapping") || !strings.Contains(result.Err().Error(), "excessive aliasing") {
			t.Fatal("cache formatting discarded a joined diagnostic", result.Err())
		}
		if _, rejected := errors.AsType[*codec.AdmissionError](result.Err()); !rejected {
			t.Fatal("cache discarded admission rejection cause")
		}
	}
}

func TestMetadataCachePreservesNativeLoadErrorText(t *testing.T) {
	var fatal strings.Builder
	fatal.WriteString("name: rejected\ndefinitions: &definitions\n")
	for input := range 650 {
		fmt.Fprintf(&fatal, "  input%d: {description: value}\n", input)
	}
	fatal.WriteString("inputs: *definitions\nruns: {using: composite, steps: []}\n")
	for _, source := range []string{"name: [\n", fatal.String()} {
		_, original := actionmeta.Parse("action.yml", []byte(source))
		if _, native := errors.AsType[*codec.LoadError](original); !native {
			t.Fatal("fixture needs a native load failure", original)
		}
		cache := newCapturedActionCache(t, t.TempDir(), nil)
		cache.Configure(Options{CustomReader: true, Reader: fsread.New(t.Context(), func(string) ([]byte, error) {
			return []byte(source), nil
		}, nil)})
		result, _ := cache.LookupMetadata("./rejected")
		if result.Status() != MetadataParseFailed || result.Err() == nil || !strings.Contains(result.Err().Error(), original.Error()) {
			t.Fatalf("cache discarded native stage/context: got %v, want %v", result.Err(), original)
		}
	}
}

func TestMetadataReadCancellationDoesNotPoisonCache(t *testing.T) {
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, docker := range []bool{false, true} {
			name := failure.Error() + "/manifest"
			if docker {
				name = failure.Error() + "/dockerfile"
			}
			t.Run(name, func(t *testing.T) {
				cache := newCapturedActionCache(t, t.TempDir(), nil)
				failed := false
				cache.Configure(Options{CustomReader: true, Reader: fsread.New(t.Context(), func(path string) ([]byte, error) {
					if docker && filepath.Base(path) != "Dockerfile" {
						return nil, os.ErrNotExist
					}
					if !failed {
						failed = true
						return nil, failure
					}
					if docker {
						return []byte("FROM scratch\n"), nil
					}
					return []byte("name: retry\ndescription: test\nruns: {using: composite, steps: []}\n"), nil
				}, nil)})
				if _, cached, err := cache.FindMetadata("./retry"); cached || !errors.Is(err, failure) {
					t.Fatalf("first lookup: cached=%v error=%v", cached, err)
				}
				if _, exists := cache.readCache("./retry"); exists {
					t.Fatal("interrupted read published a permanent cache entry")
				}
				if !docker {
					if _, _, err := cache.FindMetadata("./retry"); !errors.Is(err, failure) {
						t.Fatalf("result capture silently retried its failed revision: %v", err)
					}
					// A new result gets a new capture owner; the metadata cache must
					// still recover because it did not publish the interrupted entry.
					bindCapturedActionSources(t, cache)
				}
				want := MetadataResolved
				if docker {
					want = MetadataDockerfileOnly
				}
				for _, wantCached := range []bool{false, true} {
					result, cached := cache.LookupMetadata("./retry")
					if cached != wantCached || result.Status() != want || result.Err() != nil {
						t.Fatalf("retry: status=%v cached=%v error=%v", result.Status(), cached, result.Err())
					}
				}
			})
		}
	}
}

func TestCaseInsensitiveResolutionPreservesInterruption(t *testing.T) {
	for _, mapped := range []bool{false, true} {
		for _, deadline := range []bool{false, true} {
			t.Run(fmt.Sprintf("mapped=%t/deadline=%t", mapped, deadline), func(t *testing.T) {
				root := t.TempDir()
				writeFixture(t, root, "Target/action.yml", "name: retry\ndescription: test\nruns: {using: composite, steps: []}\n")
				ctx, cancel := context.WithCancel(t.Context())
				if deadline {
					cancel()
					ctx, cancel = context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
				}
				cancel()
				cache := newCapturedActionCache(t, root, nil)
				cache.Configure(Options{Reader: fsread.New(ctx, nil, nil)})
				resolver := NewResolver(cache, nil)
				resolver.caseInsensitive = true
				spec := "$/target"
				if mapped {
					resolver.sourceMappings = map[string]string{"./alias": "./target"}
					spec = "$/alias"
				}
				result, cached := resolver.LookupMetadata(spec)
				if cached || result.Status() != MetadataReadFailed || !errors.Is(result.Err(), ctx.Err()) {
					t.Fatalf("resolver disguised interruption as a missing action: status=%v cached=%v error=%v", result.Status(), cached, result.Err())
				}
				cache.Configure(Options{Reader: fsread.New(t.Context(), nil, nil)})
				result, cached = resolver.LookupMetadata(spec)
				if cached || result.Status() != MetadataResolved || result.Err() != nil {
					t.Fatalf("interrupted resolution poisoned retry: status=%v cached=%v error=%v", result.Status(), cached, result.Err())
				}
			})
		}
	}
}

func TestDockerfileReadFailureSurvivesReporting(t *testing.T) {
	cache := newCapturedActionCache(t, t.TempDir(), nil)
	reads := 0
	cache.Configure(Options{CustomReader: true, Reader: fsread.New(t.Context(), func(path string) ([]byte, error) {
		reads++
		if filepath.Base(path) == "Dockerfile" {
			return nil, os.ErrPermission
		}
		return nil, os.ErrNotExist
	}, nil)})
	if _, cached, err := cache.FindMetadata("./denied"); cached || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("initial lookup: cached=%v error=%v", cached, err)
	}
	firstReads := reads
	result, cached := cache.LookupMetadata("./denied")
	if !cached || result.Status() != MetadataReadFailed || !errors.Is(result.Err(), os.ErrPermission) || reads != firstReads {
		t.Fatalf("lookup reread or lost failure: result=%+v cached=%v reads=%d, initial=%d", result, cached, reads, firstReads)
	}
	if _, _, err := cache.FindMetadata("./denied"); err != nil {
		t.Fatalf("repeated reporting: %v", err)
	}
}

func TestEmptyParseErrorRetainsItsPath(t *testing.T) {
	failure := NewParseError("broken/action.yml", nil)
	if got := failure.Error(); got != `could not parse action metadata in "broken/action.yml"` {
		t.Fatal(got)
	}
	if errors.Unwrap(failure) != nil {
		t.Fatal("empty parse error invented a cause")
	}
}

func TestCachedMetadataIsVisibleDuringDocumentNotification(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "action/action.yml", "name: published\ndescription: identity\nruns: {using: composite, steps: []}\n")
	cache := newCapturedActionCache(t, root, nil)
	entered, release, notified := make(chan struct{}), make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	cache.Configure(Options{OnDocument: func(_ capture.Snapshot, _ string, _ *actionmeta.Metadata, _ error) {
		close(entered)
		<-release
		close(notified)
	}})
	winner := make(chan MetadataResult, 1)
	go func() {
		result, _ := cache.LookupMetadata("./action")
		winner <- result
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("publisher never notified")
	}
	hit := make(chan MetadataResult, 1)
	go func() {
		result, cached := cache.LookupMetadata("./action")
		if !cached {
			t.Error("published metadata was not cached")
		}
		hit <- result
	}()
	var published MetadataResult
	select {
	case published = <-hit:
	case <-time.After(3 * time.Second):
		t.Fatal("cache hit blocked on document notification")
	}
	select {
	case <-notified:
		t.Fatal("observer completed before its release")
	case <-winner:
		t.Fatal("publishing lookup returned before its observer")
	default:
	}
	unblock()
	select {
	case result := <-winner:
		if result.Metadata() == nil || result.Metadata() != published.Metadata() {
			t.Fatal("concurrent lookups returned different declarations")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("publisher did not finish after its observer")
	}
	select {
	case <-notified:
	default:
		t.Fatal("publishing lookup returned without completing notification")
	}
}

func TestConcurrentFirstMetadataPublicationNotifiesOnce(t *testing.T) {
	for _, broken := range []bool{false, true} {
		name := "valid"
		content := "name: published\ndescription: identity\nruns: {using: composite, steps: []}\n"
		if broken {
			name, content = "broken", "runs: [\n"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, root, "action/action.yml", content)
			cache := newCapturedActionCache(t, root, nil)
			var notifications atomic.Int32
			cache.Configure(Options{OnDocument: func(captured capture.Snapshot, path string, metadata *actionmeta.Metadata, parseErr error) {
				notifications.Add(1)
				published, exists := cache.readCache("./action")
				cache.mu.RLock()
				raw := cache.sources["./action"]
				cache.mu.RUnlock()
				if !exists || raw.path != path || !bytes.Equal(raw.captured.Bytes(), metadata.Source) || raw.captured.Revision() != captured.Revision() {
					t.Error("observer ran before matching metadata/source publication")
				}
				if !broken && published.Metadata() != metadata {
					t.Error("observer received losing metadata")
				}
				if (parseErr != nil) != broken {
					t.Error("observer received mismatched parse status")
				}
			}})
			start := make(chan struct{})
			var workers sync.WaitGroup
			var fresh atomic.Int32
			for range 16 {
				workers.Go(func() {
					<-start
					result, cached := cache.LookupMetadata("./action")
					if !cached {
						fresh.Add(1)
					}
					published, exists := cache.readCache("./action")
					if !exists || result.Metadata() != published.Metadata() || result.Status() != published.Status() {
						t.Error("lookup returned unpublished metadata")
					}
				})
			}
			close(start)
			workers.Wait()
			if notifications.Load() != 1 || fresh.Load() != 1 {
				t.Fatalf("notifications=%d fresh=%d, want one publication", notifications.Load(), fresh.Load())
			}
		})
	}
}

func TestMetadataLookupRetainsFailureIndependentlyOfReporting(t *testing.T) {
	for _, typedFirst := range []bool{false, true} {
		name := "validation-first"
		if typedFirst {
			name = "lookup-first"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, root, "broken/action.yml", "runs: [\n")
			cache := newCapturedActionCache(t, root, nil)
			resolver := NewResolver(cache, nil)
			if typedFirst {
				result, cached := resolver.LookupMetadata("$/broken")
				if cached || result.Status() != MetadataParseFailed || result.Err() == nil {
					t.Fatalf("initial lookup: %+v cached=%v", result, cached)
				}
			}
			_, _, first := resolver.FindMetadata("$/broken")
			if _, ok := errors.AsType[*ParseError](first); !ok {
				t.Fatalf("lookup consumed validation diagnostic: %v", first)
			}
			if _, _, repeated := resolver.FindMetadata("$/broken"); repeated != nil {
				t.Fatalf("validation repeated parse diagnostic: %v", repeated)
			}
			for range 2 {
				result, cached := resolver.LookupMetadata("$/broken")
				if !cached || result.Status() != MetadataParseFailed || result.Metadata() != nil || !errors.Is(result.Err(), first) {
					t.Fatalf("cached lookup lost failure: %+v cached=%v", result, cached)
				}
			}
		})
	}
}

func TestMetadataReadFailureSurvivesReporting(t *testing.T) {
	cache := newCapturedActionCache(t, t.TempDir(), nil)
	cache.Configure(Options{CustomReader: true, Reader: fsread.New(t.Context(), func(string) ([]byte, error) {
		return nil, os.ErrPermission
	}, nil)})
	if _, _, err := cache.FindMetadata("./denied"); !errors.Is(err, os.ErrPermission) {
		t.Fatal(err)
	}
	result, cached := cache.LookupMetadata("./denied")
	if !cached || result.Status() != MetadataReadFailed || !errors.Is(result.Err(), os.ErrPermission) {
		t.Fatalf("cached read failure lost: %+v cached=%v", result, cached)
	}
}

func TestMetadataOutcomesDistinguishMissingAndUnavailable(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "docker/Dockerfile", "FROM scratch\n")
	cache := newCapturedActionCache(t, root, nil)
	resolver := NewResolver(cache, map[string]string{"./generated": "./missing-source"})
	for _, tc := range []struct {
		name     string
		lookup   func() (MetadataResult, bool)
		status   MetadataStatus
		hasError bool
	}{
		{"dockerfile", func() (MetadataResult, bool) { return resolver.LookupMetadata("$/docker") }, MetadataDockerfileOnly, false},
		{"missing", func() (MetadataResult, bool) { return resolver.LookupMetadata("$/missing") }, MetadataMissingManifest, true},
		{"mapping", func() (MetadataResult, bool) { return resolver.LookupMetadata("$/generated") }, MetadataMappingFailed, true},
		{"remote", func() (MetadataResult, bool) { return resolver.LookupMetadata("owner/repo@v1") }, MetadataNonLocal, false},
		{"disabled", func() (MetadataResult, bool) { return NewResolver(nil, nil).LookupMetadata("./action") }, MetadataLookupDisabled, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for range 2 {
				result, _ := tc.lookup()
				if result.Status() != tc.status || (result.Err() != nil) != tc.hasError || result.Metadata() != nil {
					t.Fatalf("lookup result: %+v", result)
				}
			}
		})
	}
	resolver.setCheckout(execution.Directory{Kind: execution.DirectoryUnknown})
	result, _ := resolver.LookupMetadata("./uncertain")
	if result.Status() != MetadataPlacementUncertain || result.Err() != nil {
		t.Fatalf("uncertain placement misreported as missing manifest: %+v", result)
	}
	for range 2 {
		if _, _, err := resolver.FindMetadata("$/missing"); err == nil {
			t.Fatal("missing manifest stopped reporting at each reference")
		}
	}
}

func TestMetadataDiagnosticClaimIsConcurrentAndSeparateFromLookup(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "broken/action.yml", "runs: [\n")
	cache := newCapturedActionCache(t, root, nil)
	result, _ := cache.LookupMetadata("./broken")
	if result.Status() != MetadataParseFailed {
		t.Fatalf("unexpected lookup: %+v", result)
	}
	var reports atomic.Int32
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			if _, _, err := cache.FindMetadata("./broken"); err != nil {
				reports.Add(1)
			}
		})
	}
	workers.Wait()
	if reports.Load() != 1 {
		t.Fatalf("reported %d times, want 1", reports.Load())
	}
	cached, _ := cache.LookupMetadata("./broken")
	if !errors.Is(cached.Err(), result.Err()) {
		t.Fatal("reporting changed cached acquisition cause")
	}
}
