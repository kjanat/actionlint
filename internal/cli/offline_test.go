package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

func runOfflineCommand(t *testing.T, acquisition plugininstall.Acquisition, input string, args ...string) (int, string, string) {
	t.Helper()
	var out, log bytes.Buffer
	cmd := Command{Stdin: strings.NewReader(input), Stdout: &out, Stderr: &log, Acquisition: acquisition}
	status := cmd.MainContext(t.Context(), append([]string{"actionlint"}, args...))
	return status, out.String(), log.String()
}

func TestCacheWarmThenOfflineConfigurationAndPlugin(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	const base = "https://config.example/team/base.yaml"
	const parent = "https://config.example/defaults.yaml"
	const module = "https://config.example/no-sudo.wasm"
	wasm, err := os.ReadFile("internal/plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry.SetFile(module, wasm)
	registry.SetFile(base, []byte("extends: ['../defaults.yaml']\n"))
	registry.SetFile(parent, fmt.Appendf(nil, "plugins: ['%s#sha256=%x']\nlint: {rules: {plugins: {no-sudo: error}}}\noutput: {formats: [json], summary: false}\n", module, sha256.Sum256(wasm)))
	config, workflow := configuredOutputFixture(t, "")
	if err := os.WriteFile(config, []byte("extends: ['"+base+"']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte("on: push\npermissions: {}\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: 'sudo echo ok'}]}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--config", config, "--no-tool=shellcheck", "--no-tool=ruff", workflow}
	status, out, log := runOfflineCommand(t, acquisition, "", append([]string{"cache", "warm"}, args...)...)
	if status != 0 || !strings.Contains(out, "cache_directory") {
		t.Fatalf("warm status=%d stdout=%s stderr=%s", status, out, log)
	}
	if len(registry.Requests()) == 0 {
		t.Fatal("warm command did not download dependencies")
	}
	registry.ResetRequests()
	registry.SetOffline(true)
	status, out, log = runOfflineCommand(t, acquisition, "", append([]string{"check", "--offline"}, args...)...)
	var result resultreport.CheckResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("offline status=%d stdout=%s stderr=%s: %v", status, out, log, err)
	}
	if status != 1 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "no-sudo" {
		t.Fatalf("offline status=%d stdout=%s stderr=%s", status, out, log)
	}
	if requests := registry.Requests(); len(requests) != 0 {
		t.Fatalf("offline requests: %v", requests)
	}
	status, _, log = runOfflineCommand(t, acquisition, "", append([]string{"check", "--online"}, args...)...)
	if status != 3 {
		t.Fatalf("online failed request returned %d: %s", status, log)
	}
}

func TestOfflineColdCacheWarnsWithoutRequests(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"check", "--config", "https://config.example/missing.yaml", "-"},
		{"config", "show", "--config", "https://config.example/missing.yaml"},
		{"check", "--no-config", "--plugin", "https://plugins.example/missing.wasm#sha256=" + strings.Repeat("a", 64), "-"},
		{"check", "--no-config", "--plugin", "oci://ghcr.io/example/plugin@sha256:" + strings.Repeat("a", 64), "-"},
		{"plugin", "inspect", "shellcheck"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			registry := fixtureregistry.NewPluginRegistry(t)
			acquisition := plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
			status, out, log := runOfflineCommand(t, acquisition, "on: push\npermissions: {}\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n", append([]string{"--offline"}, args...)...)
			if status != 0 || !strings.Contains(log, "warning:") || !strings.Contains(log, "--offline") {
				t.Fatalf("status=%d stdout=%s stderr=%s", status, out, log)
			}
			if requests := registry.Requests(); len(requests) != 0 {
				t.Fatalf("offline requests: %v", requests)
			}
		})
	}
}

func TestCacheWarmThenOfflineNamedAction(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	sha := strings.Repeat("a", 40)
	var requests []string
	acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir(), Connections: acquisitionhttp.Connections{Transport: actionMetadataFixture(t, sha, "", &requests)}}
	input := "on: push\npermissions: {}\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@v1, with: {unknown: value}}]}}\n"
	status, out, log := runOfflineCommand(t, acquisition, input, "cache", "warm", "--no-config", "--json", "-")
	if status != 0 {
		t.Fatalf("warm status=%d stdout=%s stderr=%s", status, out, log)
	}
	before := len(requests)
	if before == 0 {
		t.Fatalf("warm made no requests: stdout=%s stderr=%s", out, log)
	}
	refs, err := filepath.Glob(filepath.Join(acquisition.UserCacheDir, "actionlint", "action-metadata", "v2", "refs", "*.json"))
	if err != nil || len(refs) != 1 {
		t.Fatalf("warm saved references=%v err=%v stdout=%s stderr=%s requests=%v", refs, err, out, log, requests)
	}
	status, out, log = runOfflineCommand(t, acquisition, input, "check", "--offline", "--no-config", "--json", "--summary=false", "-")
	var result resultreport.CheckResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("status=%d stdout=%s stderr=%s: %v", status, out, log, err)
	}
	if status != 1 || len(result.ActionMetadata) != 1 || result.ActionMetadata[0].Commit != sha || !strings.Contains(out, "unknown") || before != len(requests) {
		t.Fatalf("offline status=%d stdout=%s stderr=%s requests=%v", status, out, log, requests[before:])
	}
}

func TestOfflineRejectsCorruptCachedPlugin(t *testing.T) {
	t.Parallel()
	cache := t.TempDir()
	digest := strings.Repeat("a", 64)
	directory := filepath.Join(cache, "actionlint", "artifacts")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, digest), []byte("wrong bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, out, log := runOfflineCommand(t, plugininstall.Acquisition{UserCacheDir: cache}, "", "check", "--offline", "--no-config", "--plugin", "https://plugins.example/plugin.wasm#sha256="+digest, "-")
	if status == 0 || !strings.Contains(log, "SHA256 mismatch") {
		t.Fatalf("status=%d stdout=%s stderr=%s", status, out, log)
	}
}

func TestOfflineHTTPGuardWithExplicitEndpoint(t *testing.T) {
	t.Parallel()
	acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir(), Connections: acquisitionhttp.Connections{Transport: actionMetadataTransport(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("offline contacted %s", r.URL)
		return nil, nil
	})}}
	status, out, log := runOfflineCommand(t, acquisition, "", "plugin", "search", "--registry", "https://example.org/", "--offline")
	if status != 0 || !strings.Contains(log, "--offline") {
		t.Fatalf("status=%d stdout=%s stderr=%s", status, out, log)
	}
}

func TestCacheWarmDoesNotReportSuccessForMissingWorkflow(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.SetOffline(true)
	acquisition := plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	input := "on: push\npermissions: {}\njobs: {call: {uses: example/workflows/.github/workflows/build.yml@" + strings.Repeat("a", 40) + "}}\n"
	status, out, log := runOfflineCommand(t, acquisition, input, "cache", "warm", "--no-config", "--json", "-")
	if status != 3 || strings.Contains(out, "cache_directory") || !strings.Contains(log, "could not cache") {
		t.Fatalf("warm status=%d stdout=%s stderr=%s", status, out, log)
	}
}
