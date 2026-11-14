package execution

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func deferredUnavailableFixture(t *testing.T) (*deferredScriptEngine, *fixtureregistry.PluginRegistry) {
	t.Helper()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	acquisition := plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	client, err := plugininstall.New(fixtureregistry.FixtureRegistryURL, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	registry.DeleteFile(pin.URL)
	registry.ResetRequests()
	return &deferredScriptEngine{gate: make(chan struct{}, 1), pin: pin, acquisition: acquisition}, registry
}

func TestDeferredScriptEngineCachesAcquisitionFailure(t *testing.T) {
	t.Parallel()
	engine, registry := deferredUnavailableFixture(t)
	var first error
	for range 8 {
		_, err := engine.get(t.Context())
		if err == nil {
			t.Fatal("missing artifact was accepted")
		}
		if first == nil {
			first = err
		} else if !errors.Is(err, first) {
			t.Fatalf("failure changed: first=%v later=%v", first, err)
		}
	}
	if requests := registry.Requests(); len(requests) != 1 || requests[0] != engine.pin.URL {
		t.Fatalf("artifact requests = %v", requests)
	}
}

func TestDeferredScriptEngineRetriesCanceledAcquisition(t *testing.T) {
	t.Parallel()
	engine, registry := deferredUnavailableFixture(t)
	transport, ok := engine.acquisition.Connections.Transport.(*http.Transport)
	if !ok {
		t.Fatal("fixture does not provide HTTP transport")
	}
	transport = transport.Clone()
	defer transport.CloseIdleConnections()
	original := transport.DialContext
	var attempts atomic.Int32
	started := make(chan struct{})
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if attempts.Add(1) == 1 {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return original(ctx, network, address)
	}
	engine.acquisition.Connections.Transport = transport
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := engine.get(ctx); done <- err }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled acquisition error = %v", err)
	}
	if _, err := engine.get(t.Context()); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("retry error = %v", err)
	}
	if attempts.Load() != 2 || len(registry.Requests()) != 1 {
		t.Fatalf("dial attempts=%d requests=%v", attempts.Load(), registry.Requests())
	}
}

func TestDeferredScriptEngineCachesInternalTimeout(t *testing.T) {
	t.Parallel()
	engine, _ := deferredUnavailableFixture(t)
	transport, ok := engine.acquisition.Connections.Transport.(*http.Transport)
	if !ok {
		t.Fatal("fixture does not provide HTTP transport")
	}
	transport = transport.Clone()
	defer transport.CloseIdleConnections()
	var attempts atomic.Int32
	transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		attempts.Add(1)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	engine.acquisition.Connections.Transport = transport
	var first error
	for range 5 {
		_, err := engine.get(t.Context())
		if err == nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("internal timeout error = %v", err)
		}
		if first == nil {
			first = err
		} else if !errors.Is(err, first) {
			t.Fatalf("failure changed: first=%v later=%v", first, err)
		}
	}
	if attempts.Load() != 1 {
		t.Fatalf("dial attempts = %d, want 1", attempts.Load())
	}
}
