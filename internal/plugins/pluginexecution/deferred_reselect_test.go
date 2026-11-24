package pluginexecution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/wasmcomponent"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

type deferredReselection struct {
	engine *deferredScriptEngine
	client *pluginregistry.Client
}

func newDeferredReselection(t *testing.T, newer ...[]byte) deferredReselection {
	t.Helper()
	registry := fixtureregistry.NewPluginRegistry(t)
	script, err := os.ReadFile("component/testdata/plugins/ruff-action-input.wasm")
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
	declared["repository"] = json.RawMessage(`"https://` + fixtureregistry.FixturePluginName + `"`)
	if metadata, err = json.Marshal(declared); err != nil {
		t.Fatal(err)
	}
	publisher := fixtureregistry.PluginPublisher{PluginID: "echo-component", BaseURL: fixtureregistry.FixtureRegistryURL, Name: fixtureregistry.FixturePluginName, RepositoryID: 10, Metadata: metadata}
	registry.AddPlugin(t, publisher, "v1.0.0", script)
	publisher.Metadata = nil
	versions := []string{"v1.0.0"}
	for i, wasm := range newer {
		version := fmt.Sprintf("v%d.0.0", i+2)
		registry.AddPlugin(t, publisher, version, wasm)
		versions = append(versions, version)
	}
	registry.PublishPlugin(t, publisher, versions...)
	acquisition := dependencycache.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	client, err := pluginregistry.New(fixtureregistry.FixtureRegistryURL, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.ResolveCached(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil || pin.Version != versions[len(versions)-1] {
		t.Fatalf("selection=%+v error=%v", pin, err)
	}
	if _, err := artifacts.Resolve(t.Context(), pin.Source(), pluginacquisition.ArtifactOptions(acquisition)); err != nil {
		t.Fatal(err)
	}
	first, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := deferredDescriptor(first, rule.Names{})
	if err != nil {
		t.Fatal(err)
	}
	engine := &deferredScriptEngine{gate: make(chan struct{}, 1), descriptor: descriptor, pin: pin, acquisition: acquisition}
	t.Cleanup(func() {
		if err := engine.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	return deferredReselection{engine: engine, client: client}
}

func checkSkipped(ctx context.Context, t *testing.T, engine *deferredScriptEngine, session *network.Session, want string) {
	t.Helper()
	if diagnostics, err := engine.Check(ctx, scriptengine.Request{}); err != nil || diagnostics != nil {
		t.Fatalf("skipped engine: diagnostics=%v error=%v", diagnostics, err)
	}
	warnings := session.Warnings()
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "Skipping default plugin: ") || !strings.Contains(warnings[0], want) {
		t.Fatalf("warnings=%q", warnings)
	}
}

func TestSkippedDeferredEngineLoadsReselectedReleaseNextAnalysis(t *testing.T) {
	t.Parallel()
	fixture := newDeferredReselection(t, wasmcomponent.Build(nil, []string{component.AnalyzerInterface}, nil))
	ctx, session := network.WithSession(t.Context(), network.Options{})
	checkSkipped(ctx, t, fixture.engine, session, "incompatible analyzer contract")
	if backend, err := fixture.engine.get(ctx); backend != nil || err != nil {
		t.Fatalf("same analysis loaded backend=%v error=%v", backend, err)
	}
	if err := fixture.engine.beginAnalysis(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if fixture.engine.pin.Version != "v1.0.0" || fixture.engine.skipped || fixture.engine.next != nil {
		t.Fatalf("next analysis pin=%s skipped=%t next=%v", fixture.engine.pin.Version, fixture.engine.skipped, fixture.engine.next)
	}
	descriptor, err := deferredDescriptor(fixture.engine.pin, rule.Names{})
	if err != nil {
		t.Fatal(err)
	}
	if !sameScriptDescriptor(fixture.engine.ScriptDescriptor(), descriptor) {
		t.Fatalf("descriptor=%+v, want %+v", fixture.engine.ScriptDescriptor(), descriptor)
	}
	backend, err := fixture.engine.get(ctx)
	if err != nil || backend == nil {
		t.Fatalf("reselected release: backend=%v error=%v", backend, err)
	}
}

func TestReselectedReleaseThatCannotLoadLazilyIsRejected(t *testing.T) {
	t.Parallel()
	fixture := newDeferredReselection(t, fixtureregistry.AnalyzerComponent(), wasmcomponent.Build(nil, []string{component.AnalyzerInterface}, nil))
	ctx, session := network.WithSession(t.Context(), network.Options{})
	checkSkipped(ctx, t, fixture.engine, session, "incompatible analyzer contract")
	if fixture.engine.next == nil || fixture.engine.next.Version != "v2.0.0" {
		t.Fatalf("reselected=%+v", fixture.engine.next)
	}
	if err := fixture.engine.beginAnalysis(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	warnings := session.Warnings()
	if len(warnings) != 2 || !strings.Contains(warnings[1], "plugin "+fixtureregistry.FixturePluginName+"@v2.0.0 does not load in this actionlint: default plugin release has no script-only metadata") {
		t.Fatalf("warnings=%q", warnings)
	}
	if fixture.engine.pin.Version != "v1.0.0" || fixture.engine.skipped || fixture.engine.next != nil {
		t.Fatalf("next analysis pin=%s skipped=%t next=%v", fixture.engine.pin.Version, fixture.engine.skipped, fixture.engine.next)
	}
	if backend, err := fixture.engine.get(ctx); err != nil || backend == nil {
		t.Fatalf("loadable release: backend=%v error=%v", backend, err)
	}
	next, err := fixture.client.ResolveCached(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil || next.Version != "v1.0.0" {
		t.Fatalf("next run: selection=%+v error=%v", next, err)
	}
}

func TestReselectedReleaseWithAnotherEngineIsRejected(t *testing.T) {
	t.Parallel()
	fixture := newDeferredReselection(t, wasmcomponent.Build(nil, []string{component.AnalyzerInterface}, nil))
	fixture.engine.descriptor.Engine = "other"
	ctx, session := network.WithSession(t.Context(), network.Options{})
	checkSkipped(ctx, t, fixture.engine, session, "incompatible analyzer contract")
	if err := fixture.engine.beginAnalysis(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	warnings := session.Warnings()
	if len(warnings) != 2 || !strings.Contains(warnings[1], `@v1.0.0 does not load in this actionlint: default plugin release provides script engine "ruff" with rule "ruff"; the session registered engine "other" with rule "ruff"`) {
		t.Fatalf("warnings=%q", warnings)
	}
	if !fixture.engine.skipped || fixture.engine.next != nil {
		t.Fatalf("skipped=%t next=%v", fixture.engine.skipped, fixture.engine.next)
	}
	if _, err := fixture.client.ResolveCached(t.Context(), fixtureregistry.FixturePluginName, "latest"); !errors.Is(err, pluginregistry.ErrNoCompatibleRelease) {
		t.Fatalf("next run error=%v", err)
	}
}

func TestReselectedReleaseWithReservedRuleIsRejected(t *testing.T) {
	t.Parallel()
	fixture := newDeferredReselection(t, wasmcomponent.Build(nil, []string{component.AnalyzerInterface}, nil))
	reserved, err := rule.NewNames([]rule.Descriptor{{Name: "ruff"}})
	if err != nil {
		t.Fatal(err)
	}
	fixture.engine.reserved = reserved
	ctx, session := network.WithSession(t.Context(), network.Options{})
	checkSkipped(ctx, t, fixture.engine, session, "incompatible analyzer contract")
	if err := fixture.engine.beginAnalysis(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	warnings := session.Warnings()
	if len(warnings) != 2 || !strings.Contains(warnings[1], `@v1.0.0 does not load in this actionlint: plugin name "ruff" conflicts with a built-in rule`) {
		t.Fatalf("warnings=%q", warnings)
	}
	if !fixture.engine.skipped || fixture.engine.next != nil {
		t.Fatalf("skipped=%t next=%v", fixture.engine.skipped, fixture.engine.next)
	}
}
