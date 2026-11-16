package component

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	runner "actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/services"
	"github.com/kjanat/wacogo"
)

const runnerSupportFixture = "testdata/plugins/runner-support-0.3.0.wasm"

func loadRunnerSupportFixture(t *testing.T) *Plugin {
	t.Helper()
	plugin, err := Load(t.Context(), runnerSupportFixture, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := plugin.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if plugin.HasAnalyzer() || !plugin.HasRunnerProvider() || plugin.runner != (runnerProtocol03{}) {
		t.Fatal("support fixture negotiated the wrong role or interface")
	}
	return plugin
}

func runnerSupportRequest(t *testing.T, kind, version string) runner.Request {
	t.Helper()
	service, err := services.Parse(kind, version)
	if err != nil {
		t.Fatal(err)
	}
	date, err := runner.ParseDate("2026-12-08")
	if err != nil {
		t.Fatal(err)
	}
	return runner.Request{
		Target: "github", Scope: "org/repo", Service: service, AsOf: date,
		Selectors: []runner.Selector{{Labels: []runner.Constraint{{Value: runner.Literal("arm")}}}},
	}
}

func TestRunnerSupportComponentRoundTrip(t *testing.T) {
	t.Parallel()
	plugin := loadRunnerSupportFixture(t)
	for _, service := range []struct{ kind, version string }{
		{"github.com", ""}, {"github-data-residency", ""}, {"ghes", "3.19"}, {"ghes", ""}, {"", ""},
	} {
		t.Run(service.kind+service.version, func(t *testing.T) {
			t.Parallel()
			request := runnerSupportRequest(t, service.kind, service.version)
			response, err := plugin.ResolveRunners(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if len(response.Answers) != 1 {
				t.Fatalf("answers = %+v", response.Answers)
			}
			resolved, ok := response.Answers[0].Resolution.(runner.Resolved)
			if !ok || len(resolved.Candidates) != 1 {
				t.Fatalf("resolution = %+v", response.Answers[0].Resolution)
			}
			candidate := resolved.Candidates[0]
			if candidate.Name != service.kind+":"+service.version+":2026-12-08" || candidate.Hosting != runner.SelfHosted || candidate.Architecture != "arm32" {
				t.Fatalf("service, version, date or candidate facts changed: %+v", candidate)
			}
			catalog := response.Support
			if len(catalog.Policies) != 1 || !catalog.DatasetStatus.Complete || catalog.DatasetStatus.Freshness != runner.Fresh || catalog.DatasetStatus.Scope != request.Scope {
				t.Fatalf("catalogue = %+v", catalog)
			}
			policy := catalog.Policies[0]
			if policy.ID != "arm32-fixture" || policy.Description != "ARM32 support retirement" || policy.URL != "https://example.com/runner-support" || policy.Capability != runner.JobExecution || policy.Service.Kind != request.Service.Kind() || policy.Initial != runner.SupportUnknown {
				t.Fatalf("policy fields changed: %+v", policy)
			}
			if !reflect.DeepEqual(policy.Hosting, []runner.Hosting{runner.SelfHosted}) || !reflect.DeepEqual(policy.OS, []runner.OS{runner.Linux, runner.Windows}) || !reflect.DeepEqual(policy.Architectures, []string{"arm32"}) {
				t.Fatalf("policy constraints changed: %+v", policy)
			}
			if len(policy.Changes) != 2 || policy.Changes[0].On.String() != "2026-11-12" || policy.Changes[0].State != runner.Deprecated || policy.Changes[1].On.String() != "2026-12-08" || policy.Changes[1].State != runner.Unsupported {
				t.Fatalf("schedule changed: %+v", policy.Changes)
			}
			if service.kind == "ghes" && (policy.Service.MinVersion != "3.18" || policy.Service.BeforeVersion != "3.20") {
				t.Fatalf("GHES version bounds changed: %+v", policy.Service)
			}
		})
	}
}

func TestRunnerSupportComponentRejectsMalformedCatalogues(t *testing.T) {
	t.Parallel()
	plugin := loadRunnerSupportFixture(t)
	for _, mode := range []string{"bad-date", "unordered", "empty-id", "unknown-service", "bad-url", "bad-version", "duplicate", "foreign-scope", "error"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			request := runnerSupportRequest(t, "github.com", "")
			request.Snapshot.Content = mode
			response, err := plugin.ResolveRunners(t.Context(), request)
			if err == nil || len(response.Answers) != 0 || len(response.Support.Policies) != 0 {
				t.Fatalf("invalid catalogue escaped admission: %+v, error=%v", response, err)
			}
		})
	}
	request := runnerSupportRequest(t, "github.com", "")
	request.Snapshot.Content = "missing"
	response, err := plugin.ResolveRunners(t.Context(), request)
	if err != nil || len(response.Answers) != 1 || response.Support.DatasetStatus.DatasetID != "" || len(response.Support.Policies) != 0 {
		t.Fatalf("absent support changed inventory facts: %+v, error=%v", response, err)
	}
	request.AsOf = runner.Date{}
	if _, err := plugin.ResolveRunners(t.Context(), request); err == nil {
		t.Fatal("support provider accepted an absent assessment date")
	}
}

func TestRunnerSupportContractAndNegotiation(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../../plugins/protocol/wit/deps/actionlint-0.3.0/runner.wit")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))) != runner03WITSHA256 {
		t.Fatal("stale support contract; regenerate")
	}
	fixture, err := os.ReadFile(runnerSupportFixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePluginInterfaces(fixture, extensionRoleSet{runner: runnerProtocol03{}}); err != nil {
		t.Fatal(err)
	}
	altered := bytes.ReplaceAll(fixture, []byte("before-version"), []byte("before-versiox"))
	if bytes.Equal(fixture, altered) {
		t.Fatal("fixture contains no service version constraint")
	}
	if err := validatePluginInterfaces(altered, extensionRoleSet{runner: runnerProtocol03{}}); err == nil {
		t.Fatal("changed nested support contract accepted")
	}
	for _, names := range [][]string{{RunnerInterface}, {Runner03Interface}, {RunnerInterface, Runner03Interface}, {"kjanat:actionlint/runner-provider@0.4.0"}} {
		exports := make([]wacogo.ExportDesc, len(names))
		for i, name := range names {
			exports[i] = wacogo.ExportDesc{Name: name, Kind: wacogo.SortInstance}
		}
		roles, err := extensionRoles(exports)
		switch {
		case len(names) > 1:
			if err == nil || !strings.Contains(err.Error(), "export one runner-provider version") {
				t.Fatalf("duplicate runner versions accepted: %+v %v", roles, err)
			}
		case names[0] == "kjanat:actionlint/runner-provider@0.4.0":
			if err == nil || !strings.Contains(err.Error(), "this host supports") {
				t.Fatalf("unknown runner version accepted: %+v %v", roles, err)
			}
		case err != nil || roles.runner.interfaceName() != names[0]:
			t.Fatalf("runner version negotiation failed: %+v %v", roles, err)
		}
	}
}
