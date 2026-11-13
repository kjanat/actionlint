package source

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
)

type configCacheTransport struct{ requests *atomic.Int32 }

func (transport configCacheTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.requests.Add(1)
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("output: {summary: false}")), Header: make(http.Header), Request: request}, nil
}

func TestReadConfigCachesHTTPSAcrossReaders(t *testing.T) {
	t.Parallel()
	ctx := WithReadCache(t.Context())
	var requests atomic.Int32
	for range 3 {
		reader := New(ctx, nil, acquisitionhttp.Connections{})
		reader.client.Transport = configCacheTransport{requests: &requests}
		if _, err := reader.ReadConfig("https://example.com/actionlint.yaml"); err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("made %d requests for one config source", requests.Load())
	}
}

func TestReadCacheCapturesOneCommand(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "actionlint.yaml")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := WithReadCache(t.Context())
	first, err := New(ctx, nil, acquisitionhttp.Connections{}).ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	first[0] = 'X'
	if err := os.WriteFile(path, []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	captured, err := New(ctx, nil, acquisitionhttp.Connections{}).ReadConfig(path)
	if err != nil || string(captured) != "first" {
		t.Fatalf("captured=%q err=%v", captured, err)
	}
	fresh, err := New(WithReadCache(t.Context()), nil, acquisitionhttp.Connections{}).ReadConfig(path)
	if err != nil || string(fresh) != "second" {
		t.Fatalf("fresh=%q err=%v", fresh, err)
	}
	current, err := New(ctx, nil, acquisitionhttp.Connections{}).ReadFile(path)
	if err != nil || string(current) != "second" {
		t.Fatalf("ordinary file read used the config snapshot: %q, %v", current, err)
	}
}

func TestReadCacheDoesNotCacheCustomReaders(t *testing.T) {
	t.Parallel()
	var calls int
	read := func(string) ([]byte, error) { calls++; return []byte("custom"), nil }
	reader := New(WithReadCache(t.Context()), read, acquisitionhttp.Connections{})
	for range 2 {
		if _, err := reader.ReadFile("actionlint.yaml"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("custom reader called %d times", calls)
	}
}

func TestReadCacheWaitCancellationAndRetry(t *testing.T) {
	t.Parallel()
	cache := &readCache{entries: make(map[string]*cachedRead)}
	started, release, completed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	go func() {
		defer close(completed)
		_, _ = cache.read(t.Context(), "source", func() ([]byte, error) {
			calls.Add(1)
			close(started)
			<-release
			return []byte("captured"), nil
		})
	}()
	<-started
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := cache.read(ctx, "source", func() ([]byte, error) { t.Error("duplicate read"); return nil, nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait: %v", err)
	}
	close(release)
	<-completed
	got, err := cache.read(t.Context(), "source", func() ([]byte, error) { t.Error("duplicate read"); return nil, nil })
	if err != nil || string(got) != "captured" || calls.Load() != 1 {
		t.Fatalf("result=%q calls=%d err=%v", got, calls.Load(), err)
	}
	failure := errors.New("read failed")
	if _, err := cache.read(t.Context(), "retry", func() ([]byte, error) { return nil, failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if _, err := cache.read(t.Context(), "retry", func() ([]byte, error) { return []byte("retry worked"), nil }); err != nil {
		t.Fatal(err)
	}
}
