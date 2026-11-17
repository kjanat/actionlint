package pluginexecution

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

func TestDeferredFailureExpiresAtAnalysisBoundary(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	component, err := os.ReadFile("component/testdata/plugins/ruff-action-input.wasm")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := os.ReadFile("component/testdata/sources/script-engine/ruff-action-input.metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	var declared map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &declared); err != nil {
		t.Fatal(err)
	}
	declared["repository"] = json.RawMessage(`"https://github.com/publisher/plugin"`)
	metadata, err = json.Marshal(declared)
	if err != nil {
		t.Fatal(err)
	}
	bytes := registry.AddPlugin(t, fixtureregistry.PluginPublisher{PluginID: "echo-component", BaseURL: fixtureregistry.FixtureRegistryURL, Name: "github.com/publisher/plugin", RepositoryID: 10, Metadata: metadata}, "v1.0.0", component)
	acquisition := dependencycache.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	client, err := pluginregistry.New(fixtureregistry.FixtureRegistryURL, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), "github.com/publisher/plugin", "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := newDeferredScriptEngine(pin, acquisition, rule.Names{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := engine.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	registry.DeleteFile(pin.URL)
	registry.ResetRequests()
	_, failure := engine.get(t.Context())
	if failure == nil {
		t.Fatal("unavailable artifact loaded")
	}
	registry.SetFile(pin.URL, bytes)
	retryAfter := engine.retryAfter
	if retryAfter.IsZero() {
		t.Fatal("failure has no retry time")
	}
	if err := engine.beginAnalysis(t.Context(), retryAfter.Add(-time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.get(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("early retry error = %v", err)
	}
	if requests := registry.Requests(); len(requests) != 1 {
		t.Fatalf("early requests = %v", requests)
	}
	// The elapsed retry time alone does not clear a failure during analysis.
	engine.retryAfter = time.Now().Add(-time.Second)
	if _, err := engine.get(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("same analysis retried: %v", err)
	}
	if requests := registry.Requests(); len(requests) != 1 {
		t.Fatalf("same analysis requests = %v", requests)
	}
	if err := engine.beginAnalysis(t.Context(), retryAfter); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.get(t.Context()); err != nil {
		t.Fatalf("later analysis failed to recover: %v", err)
	}
	if requests := registry.Requests(); len(requests) != 2 {
		t.Fatalf("recovery requests = %v", requests)
	}
	if _, err := engine.get(t.Context()); err != nil || len(registry.Requests()) != 2 {
		t.Fatalf("loaded engine was not reused: %v; requests=%v", err, registry.Requests())
	}
}

func TestBeginAnalysisHonorsCancellation(t *testing.T) {
	t.Parallel()
	engine := &deferredScriptEngine{gate: make(chan struct{}, 1), failure: errors.New("failed"), retryAfter: time.Now().Add(-time.Second)}
	engine.gate <- struct{}{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := engine.beginAnalysis(ctx, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	<-engine.gate
	if engine.failure == nil {
		t.Fatal("canceled analysis cleared failure")
	}
}

func TestLoadedBeginAnalysisClearsExpiredFailures(t *testing.T) {
	t.Parallel()
	engine, registry := deferredUnavailableFixture(t)
	if _, err := engine.get(t.Context()); err == nil {
		t.Fatal("unavailable artifact loaded")
	}
	engine.retryAfter = time.Now().Add(-time.Second)
	loaded := &Loaded{deferred: []*deferredScriptEngine{engine}}
	if err := loaded.BeginAnalysis(t.Context()); err != nil {
		t.Fatal(err)
	}
	if engine.failure != nil || len(registry.Requests()) != 1 {
		t.Fatalf("analysis boundary failure=%v requests=%v", engine.failure, registry.Requests())
	}
	if _, err := engine.get(t.Context()); err == nil || len(registry.Requests()) != 2 {
		t.Fatalf("later acquisition error=%v requests=%v", err, registry.Requests())
	}
}
