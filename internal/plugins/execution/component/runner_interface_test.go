package component

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	runner "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

func loadRunnerFixture(t *testing.T) *Plugin {
	t.Helper()
	p, err := Load(t.Context(), "testdata/plugins/buildkite-runners.wasm", Options{})
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
	if err := validatePluginInterfaces(data, extensionRoleSet{runner: true}); err != nil {
		t.Fatal(err)
	}
	altered := bytes.ReplaceAll(data, []byte("case-sensitivity"), []byte("case-sensitivitx"))
	if bytes.Equal(data, altered) {
		t.Fatal("fixture contains no candidate field")
	}
	if err := validatePluginInterfaces(altered, extensionRoleSet{runner: true}); err == nil {
		t.Fatal("changed nested provider contract accepted")
	}
}

func TestPackagedRunnerUsesItsOwnDescriptorAndSnapshotScope(t *testing.T) {
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
		ID         string            `json:"id"`
		Descriptor runner.Descriptor `json:"descriptor"`
	}
	if err := json.Unmarshal(metadata, &declared); err != nil {
		t.Fatal(err)
	}
	actual := plugin.RunnerDescriptor()
	if declared.ID != "example/buildkite-runners" || declared.ID == actual.Name || declared.Descriptor.Name != actual.Name || declared.Descriptor.Version != actual.Version {
		t.Fatalf("package and provider identities were conflated: metadata=%+v runtime=%+v", declared, actual)
	}
	request := runner.Request{
		Target: "buildkite", Scope: "org/repo",
		Snapshot:  runner.Snapshot{Scope: "org/repo", Freshness: runner.Fresh, Complete: true, Content: `{"queues":[{"name":"windows-queue","labels":["windows"],"os":"windows","architecture":"x64","shell":"pwsh"}]}`},
		Selectors: []runner.Selector{{Labels: []runner.Constraint{{Value: runner.Literal("windows"), Position: sourcepos.Position{Line: 4, Column: 15}}}}},
	}
	answers, err := plugin.ResolveRunners(t.Context(), request)
	if err != nil || len(answers) != 1 {
		t.Fatalf("answers=%+v error=%v", answers, err)
	}
	resolved, ok := answers[0].Resolution.(runner.Resolved)
	if !ok || len(resolved.Candidates) != 1 || resolved.Candidates[0].OS != runner.Windows || resolved.Candidates[0].DefaultShell != "pwsh" {
		t.Fatalf("queue platform was not supplied through the runner interface: %+v", answers)
	}
	request.Snapshot.Scope = "other/repo"
	answers, err = plugin.ResolveRunners(t.Context(), request)
	if err != nil || len(answers) != 1 {
		t.Fatalf("answers=%+v error=%v", answers, err)
	}
	if _, unknown := answers[0].Resolution.(runner.Unknown); !unknown || answers[0].DatasetStatus.Complete || answers[0].DatasetStatus.Freshness != runner.Missing {
		t.Fatalf("foreign snapshot claimed complete runner knowledge: %+v", answers)
	}
}
