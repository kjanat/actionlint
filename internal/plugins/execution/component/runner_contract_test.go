package component

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
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
	data, err := os.ReadFile("../../../../plugins/protocol/wit/runner.wit")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))) != runnerContractDigest {
		t.Fatal("stale runner contract; regenerate")
	}
	data, err = os.ReadFile("testdata/plugins/buildkite-runners.wasm")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRoleContracts(data, extensionRoleSet{runner: true}); err != nil {
		t.Fatal(err)
	}
	altered := bytes.ReplaceAll(data, []byte("case-sensitivity"), []byte("case-sensitivitx"))
	if bytes.Equal(data, altered) {
		t.Fatal("fixture contains no candidate field")
	}
	if err := checkRoleContracts(altered, extensionRoleSet{runner: true}); err == nil {
		t.Fatal("changed nested provider contract accepted")
	}
}
