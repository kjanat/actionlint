package component

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

func loadRunnerFixture(t *testing.T) *Plugin {
	t.Helper()
	p, err := LoadComponentPlugin(t.Context(), "testdata/plugins/buildkite-runners.wasm", Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if p.HasAnalyzer() || !p.HasRunnerProvider() {
		t.Fatal("provider-only component advertised wrong roles")
	}
	return p
}

func TestRunnerContractShapeAndDigest(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../../plugins/protocol/wit/runner.wit")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))) != runnerWITSHA256 {
		t.Fatal("stale runner contract; regenerate")
	}
	data, err = os.ReadFile("testdata/plugins/buildkite-runners.wasm")
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePluginInterfaces(data, extensionRoleSet{runner: runnerProtocol02{}}); err != nil {
		t.Fatal(err)
	}
	altered := bytes.ReplaceAll(data, []byte("case-sensitivity"), []byte("case-sensitivitx"))
	if bytes.Equal(data, altered) {
		t.Fatal("fixture contains no candidate field")
	}
	if err := validatePluginInterfaces(altered, extensionRoleSet{runner: runnerProtocol02{}}); err == nil {
		t.Fatal("changed nested provider contract accepted")
	}
}

func TestPackagedRunnerSeparatesPackageIDAndRejectsForeignScope(t *testing.T) {
	t.Parallel()
	plugin := loadRunnerFixture(t)
	data, err := os.ReadFile("testdata/plugins/buildkite-runners.wasm")
	if err != nil {
		t.Fatal(err)
	}
	_, metadata, err := artifacts.Sniff(data)
	if err != nil {
		t.Fatal(err)
	}
	var declared struct {
		ID         string               `json:"id"`
		Descriptor runnerapi.Descriptor `json:"descriptor"`
	}
	if err := json.Unmarshal(metadata, &declared); err != nil {
		t.Fatal(err)
	}
	actual := plugin.RunnerDescriptor()
	if declared.ID != "example/buildkite-runners" || declared.ID == actual.Name || declared.Descriptor.Name != actual.Name || declared.Descriptor.Version != actual.Version {
		t.Fatalf("package and provider identities were conflated: metadata=%+v runtime=%+v", declared, actual)
	}
	request := runnerapi.Request{
		Target: "buildkite", Scope: "org/repo",
		Snapshot:  runnerapi.RunnerSnapshot{Scope: "org/repo", Freshness: runnerapi.Fresh, Complete: true, Content: `{"queues":[{"name":"windows-queue","labels":["windows"],"os":"windows","architecture":"x64","shell":"pwsh"}]}`},
		Selectors: []runnerapi.Selector{{Labels: []runnerapi.Constraint{{Value: runnerapi.Literal("windows"), Position: sourcetext.Position{Line: 4, Column: 15}}}}},
	}
	response, err := plugin.ResolveRunners(t.Context(), request)
	answers := response.Answers
	if err != nil || len(answers) != 1 {
		t.Fatalf("answers=%+v error=%v", answers, err)
	}
	resolved, ok := answers[0].Resolution.(runnerapi.Resolved)
	if !ok || len(resolved.Candidates) != 1 || resolved.Candidates[0].OS != runnerapi.Windows || resolved.Candidates[0].DefaultShell != "pwsh" {
		t.Fatalf("queue platform was not supplied through the runner interface: %+v", answers)
	}
	if plugin.runner != (runnerProtocol02{}) || resolved.Candidates[0].Hosting != runnerapi.UnknownHosting || len(response.Support.Policies) != 0 || response.Support.DatasetStatus.DatasetID != "" {
		t.Fatalf("legacy provider invented support facts: %+v", response)
	}
	request.Snapshot.Scope = "other/repo"
	response, err = plugin.ResolveRunners(t.Context(), request)
	answers = response.Answers
	if err != nil || len(answers) != 1 {
		t.Fatalf("answers=%+v error=%v", answers, err)
	}
	if _, unknown := answers[0].Resolution.(runnerapi.Unknown); !unknown || answers[0].DatasetStatus.Complete || answers[0].DatasetStatus.Freshness != runnerapi.Missing {
		t.Fatalf("foreign snapshot claimed complete runner knowledge: %+v", answers)
	}
}
