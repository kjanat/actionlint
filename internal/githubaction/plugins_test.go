package githubaction

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/localapp"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func isolatePluginCache(t *testing.T) string {
	t.Helper()
	cache := t.TempDir()
	t.Setenv("HOME", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("LocalAppData", cache)
	base, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(base, "actionlint", "artifacts")
}

func TestActionLockedPlugins(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "discovered", true: "explicit"}[explicit], func(t *testing.T) {
			wasm, err := os.ReadFile("../plugins/component/testdata/plugins/no-sudo.wasm")
			if err != nil {
				t.Fatal(err)
			}
			registry := testutil.NewPluginRegistry(t)
			directory := isolatePluginCache(t)
			registry.Add(t, "v1.0.0", wasm)
			client, err := pluginregistry.New(testutil.FixtureRegistryURL)
			if err != nil {
				t.Fatal(err)
			}
			pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest")
			if err != nil {
				t.Fatal(err)
			}
			workspace := workspaceWith(t, map[string]string{
				".git": "", "ci.yml": "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo echo test\n",
				".github/base.yaml":       "plugins:\n  - name: " + testutil.FixturePluginName + "\n    registry: " + testutil.FixtureRegistryURL + "\n",
				".github/actionlint.yaml": "extends: [base.yaml]\n",
			})
			lock, err := pluginregistry.OpenLock(t.Context(), filepath.Join(workspace, ".github", pluginregistry.LockName))
			if err != nil {
				t.Fatal(err)
			}
			if err := lock.Set(pin); err != nil {
				t.Fatal(err)
			}
			if err := lock.Save(t.Context()); err != nil {
				t.Fatal(err)
			}
			env := map[string]string{
				"GITHUB_WORKSPACE": workspace, "INPUT_FILES": "ci.yml", "INPUT_SHELLCHECK": "false", "INPUT_RUFF": "false",
				"INPUT_CONFIG": "lint: {rules: {plugins: {no-sudo: warn}}}",
			}
			if explicit {
				env["INPUT_CONFIG-FILE"] = ".github/actionlint.yaml"
			}
			lookup := func(key string) string { return env[key] }
			registry.Requests, registry.Offline = nil, true
			var stdout, stderr bytes.Buffer
			if code := PluginPlan(lookup, &stdout, &stderr); code != 0 {
				t.Fatalf("plugin plan %d: %s", code, &stderr)
			}
			var plan localapp.PluginCachePlan
			if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
				t.Fatal(err)
			}
			if plan.Directory != directory || len(plan.Artifacts) != 1 || plan.Artifacts[0] != pin.SHA256 || len(registry.Requests) != 0 {
				t.Fatalf("plan=%+v requests=%v", plan, registry.Requests)
			}
			registry.Offline = false
			req, err := provisioningRequest(lookup)
			if err != nil {
				t.Fatal(err)
			}
			cold := runLinter(req)
			if cold.code != 1 || len(cold.diagnostics) != 1 || cold.diagnostics[0].Rule != "no-sudo" {
				t.Fatalf("cold component result: %+v, %s", cold.diagnostics, cold.stderr)
			}
			if len(registry.Requests) != 1 || registry.Requests[0] != pin.URL {
				t.Fatalf("analysis resolved registry or fetched repeatedly: %v", registry.Requests)
			}
			registry.Requests, registry.Offline = nil, true
			result := runLinter(req)
			if result.code != 1 || len(result.diagnostics) != 1 || result.diagnostics[0].Rule != "no-sudo" {
				t.Fatalf("locked component result: %+v, %s", result.diagnostics, result.stderr)
			}
			if len(registry.Requests) != 0 {
				t.Fatalf("cached Action fetched artifacts: %v", registry.Requests)
			}
			env["INPUT_CONFIG"] = "plugins: []"
			req, err = provisioningRequest(lookup)
			if err != nil {
				t.Fatal(err)
			}
			result = runLinter(req)
			if result.code != 0 || len(result.diagnostics) != 0 {
				t.Fatalf("inline plugin reset ignored: %+v %s", result.diagnostics, result.stderr)
			}
		})
	}
}

func TestActionConfiguredWasm(t *testing.T) {
	for _, fixture := range []string{"../plugins/core/testdata/engines/echo.wasm", "../plugins/component/testdata/plugins/script-engine.wasm"} {
		t.Run(fixture, func(t *testing.T) {
			isolatePluginCache(t)
			module, err := filepath.Abs(fixture)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(module)
			if err != nil {
				t.Fatal(err)
			}
			workspace := workspaceWith(t, map[string]string{
				".git": "", "ci.yml": "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\n",
				".github/actionlint.yaml": "tools:\n  ruff:\n    backend:\n      wasm:\n        path: " + string(encoded) + "\n",
			})
			t.Setenv("PATH", t.TempDir())
			env := map[string]string{"GITHUB_WORKSPACE": workspace, "INPUT_FILES": "ci.yml", "INPUT_RUFF": "true", "INPUT_SHELLCHECK": "false"}
			lookup := func(key string) string { return env[key] }
			req, err := provisioningRequest(lookup)
			if err != nil {
				t.Fatal(err)
			}
			result := runLinter(req)
			wantCode := "ECHO001"
			if strings.Contains(fixture, "/core/") {
				wantCode = "F821"
			}
			if result.code != 1 || len(result.diagnostics) != 1 || result.diagnostics[0].Code != wantCode {
				t.Fatalf("configured Wasm result: %+v %s", result.diagnostics, result.stderr)
			}
			env["INPUT_RUFF"] = "false"
			req, err = provisioningRequest(lookup)
			if err != nil {
				t.Fatal(err)
			}
			result = runLinter(req)
			if result.code != 0 {
				t.Fatalf("disabled engine ran: %+v %s", result.diagnostics, result.stderr)
			}
		})
	}
}

func TestActionEnabledRuffDoesNotRequireNativeExecutable(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(strconv.FormatBool(configured), func(t *testing.T) {
			files := map[string]string{
				".git":   "",
				"ci.yml": "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\n",
			}
			if configured {
				files[".github/actionlint.yaml"] = "tools: {ruff: {backend: {process: {executable: missing-ruff}}}}\n"
			}
			workspace := workspaceWith(t, files)
			t.Setenv("PATH", t.TempDir())
			env := map[string]string{"GITHUB_WORKSPACE": workspace, "INPUT_FILES": "ci.yml", "INPUT_RUFF": "true", "INPUT_SHELLCHECK": "false"}
			req, err := provisioningRequest(func(key string) string { return env[key] })
			if err != nil {
				t.Fatal(err)
			}
			result := runLinter(req)
			if configured {
				if result.code != 3 || !strings.Contains(result.stderr, "tools.ruff.backend.process requires host opt-in") {
					t.Fatalf("repository process was not rejected before execution: %+v", result.lintOutcome)
				}
			} else if result.code != 0 {
				t.Fatalf("enabled Ruff required a native executable: %+v", result.lintOutcome)
			}
		})
	}
}

func TestActionPluginPlanConfiguredDigests(t *testing.T) {
	directory := isolatePluginCache(t)
	digest := strings.Repeat("a", 64)
	workspace := workspaceWith(t, map[string]string{
		".git": "", ".github/actionlint.yaml": "tools:\n  ruff:\n    backend:\n      wasm:\n        path: https://example.invalid/ruff.wasm#sha256=" + digest + "\n",
	})
	env := map[string]string{"GITHUB_WORKSPACE": workspace, "INPUT_RUFF": "true"}
	var stdout, stderr bytes.Buffer
	if code := PluginPlan(func(key string) string { return env[key] }, &stdout, &stderr); code != 0 {
		t.Fatalf("plan failed: %d %s", code, &stderr)
	}
	var plan localapp.PluginCachePlan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Directory != directory || len(plan.Artifacts) != 1 || plan.Artifacts[0] != digest {
		t.Fatalf("configured digest missing: %+v", plan)
	}
}

func TestActionInlinePinnedPlugin(t *testing.T) {
	isolatePluginCache(t)
	wasm, err := os.ReadFile("../plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	url := "https://plugins.example/no-sudo.wasm"
	digest := fmt.Sprintf("%x", sha256.Sum256(wasm))
	registry := testutil.NewPluginRegistry(t)
	registry.Files[url] = wasm
	workspace := workspaceWith(t, map[string]string{
		"ci.yml": "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo echo test\n",
	})
	env := map[string]string{
		"GITHUB_WORKSPACE": workspace, "INPUT_FILES": "ci.yml", "INPUT_SHELLCHECK": "false", "INPUT_RUFF": "false",
		"INPUT_CONFIG": fmt.Sprintf("plugins: [%q]\n", url+"#sha256="+digest),
	}
	lookup := func(key string) string { return env[key] }
	var stdout, stderr bytes.Buffer
	if code := PluginPlan(lookup, &stdout, &stderr); code != 0 || len(registry.Requests) != 0 {
		t.Fatalf("inline plan fetched: %d %s %v", code, &stderr, registry.Requests)
	}
	if !strings.Contains(stdout.String(), digest) {
		t.Fatalf("inline digest missing: %s", &stdout)
	}
	req, err := provisioningRequest(lookup)
	if err != nil {
		t.Fatal(err)
	}
	cold := runLinter(req)
	if cold.code != 1 || len(cold.diagnostics) != 1 || cold.diagnostics[0].Rule != "no-sudo" {
		t.Fatalf("inline cold plugin failed: %+v %s", cold.diagnostics, cold.stderr)
	}
	if len(registry.Requests) != 1 || registry.Requests[0] != url {
		t.Fatalf("inline plugin fetches: %v", registry.Requests)
	}
	registry.Requests, registry.Offline = nil, true
	result := runLinter(req)
	if result.code != 1 || len(result.diagnostics) != 1 || result.diagnostics[0].Rule != "no-sudo" || len(registry.Requests) != 0 {
		t.Fatalf("inline cached plugin failed: %+v %s requests=%v", result.diagnostics, result.stderr, registry.Requests)
	}
}
