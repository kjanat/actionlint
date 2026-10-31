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
				"GITHUB_WORKSPACE": workspace, "INPUT_FILES": "ci.yml", "INPUT_DISABLE-TOOLS": "shellcheck,ruff",
				"INPUT_CONFIG": "lint: {rules: {plugins: {no-sudo: warn}}}",
			}
			if explicit {
				env["INPUT_CONFIG-FILE"] = ".github/actionlint.yaml"
			}
			lookup := func(key string) string { return env[key] }
			registry.ResetRequests()
			registry.SetOffline(true)
			var stdout, stderr bytes.Buffer
			if code := PluginPlan(lookup, &stdout, &stderr); code != 0 {
				t.Fatalf("plugin plan %d: %s", code, &stderr)
			}
			var plan localapp.PluginCachePlan
			if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
				t.Fatal(err)
			}
			if plan.Directory != directory || len(plan.Artifacts) != 1 || plan.Artifacts[0] != pin.SHA256 || len(registry.Requests()) != 0 {
				t.Fatalf("plan=%+v requests=%v", plan, registry.Requests())
			}
			registry.SetOffline(false)
			req, err := provisioningRequest(lookup)
			if err != nil {
				t.Fatal(err)
			}
			cold := runLinter(req)
			if cold.code != 1 || len(cold.diagnostics) != 1 || cold.diagnostics[0].Rule != "no-sudo" {
				t.Fatalf("cold component result: %+v, %s", cold.diagnostics, cold.stderr)
			}
			if len(registry.Requests()) != 1 || registry.Requests()[0] != pin.URL {
				t.Fatalf("analysis resolved registry or fetched repeatedly: %v", registry.Requests())
			}
			registry.ResetRequests()
			registry.SetOffline(true)
			result := runLinter(req)
			if result.code != 1 || len(result.diagnostics) != 1 || result.diagnostics[0].Rule != "no-sudo" {
				t.Fatalf("locked component result: %+v, %s", result.diagnostics, result.stderr)
			}
			if len(registry.Requests()) != 0 {
				t.Fatalf("cached Action fetched artifacts: %v", registry.Requests())
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
	for _, fixture := range []string{"../plugins/core/testdata/engines/echo.wasm", "../plugins/component/testdata/plugins/ruff-action-input.wasm"} {
		t.Run(fixture, func(t *testing.T) {
			isolatePluginCache(t)
			module, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(fixture, "/core/") {
				module, err = testutil.ReplaceWasmDataString(module, []byte(`"engine":{"name":"echo"`), []byte(`"engine":{"name":"ruff"`))
				if err != nil {
					t.Fatal(err)
				}
			}
			modulePath := filepath.Join(t.TempDir(), "ruff.wasm")
			if err := os.WriteFile(modulePath, module, 0o600); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(modulePath)
			if err != nil {
				t.Fatal(err)
			}
			workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\n"
			if strings.Contains(fixture, "/component/") {
				workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/execute@v1\n        with:\n          code: missing();\n"
			}
			workspace := workspaceWith(t, map[string]string{
				".git": "", "ci.yml": workflow,
				".github/actionlint.yaml": "tools:\n  ruff:\n    backend:\n      wasm:\n        path: " + string(encoded) + "\n",
			})
			t.Setenv("PATH", t.TempDir())
			env := map[string]string{"GITHUB_WORKSPACE": workspace, "INPUT_FILES": "ci.yml", "INPUT_DISABLE-TOOLS": "shellcheck"}
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
			env["INPUT_DISABLE-TOOLS"] = "shellcheck,ruff"
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
			env := map[string]string{"GITHUB_WORKSPACE": workspace, "INPUT_FILES": "ci.yml", "INPUT_DISABLE-TOOLS": "shellcheck"}
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
	env := map[string]string{"GITHUB_WORKSPACE": workspace}
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

func TestActionPluginPlanPreviewsLockOnlyLegacyMigration(t *testing.T) {
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
		".git":                    "",
		".github/actionlint.yaml": "policy: {require-job-timeout: false}\n",
		"ci.yml":                  "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo echo test\n",
	})
	lockPath := filepath.Join(workspace, ".github", pluginregistry.LockName)
	lock, err := pluginregistry.OpenLock(t.Context(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Set(pin); err != nil {
		t.Fatal(err)
	}
	if err := lock.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(workspace, ".github", "actionlint.yaml")
	configBefore, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	registry.ResetRequests()
	env := map[string]string{
		"GITHUB_WORKSPACE":    workspace,
		"INPUT_FILES":         "ci.yml",
		"INPUT_DISABLE-TOOLS": "ruff,shellcheck",
	}
	lookup := func(key string) string { return env[key] }
	var stdout, stderr bytes.Buffer
	if code := PluginPlan(lookup, &stdout, &stderr); code != 0 {
		t.Fatalf("plugin plan failed: %d %s", code, &stderr)
	}
	var plan localapp.PluginCachePlan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Directory != directory || len(plan.Artifacts) != 1 || plan.Artifacts[0] != pin.SHA256 {
		t.Fatalf("lock-only plan = %+v; want directory %s and digest %s", plan, directory, pin.SHA256)
	}
	if len(registry.Requests()) != 0 {
		t.Fatalf("cache preview fetched artifacts: %v", registry.Requests())
	}
	configAfter, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	lockAfter, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(configBefore, configAfter) || !bytes.Equal(lockBefore, lockAfter) {
		t.Fatal("cache preview modified legacy configuration or lock")
	}

	req, err := provisioningRequest(lookup)
	if err != nil {
		t.Fatal(err)
	}
	result := runLinter(req)
	if result.code != 1 || len(result.diagnostics) != 1 || result.diagnostics[0].Rule != "no-sudo" {
		t.Fatalf("migrated locked plugin result: %+v %s", result.diagnostics, result.stderr)
	}
	if len(registry.Requests()) != 1 || registry.Requests()[0] != pin.URL {
		t.Fatalf("migration fetched unexpected plugin bytes: %v", registry.Requests())
	}
	configAfter, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configAfter), testutil.FixturePluginName) {
		t.Fatalf("action analysis did not migrate pinned plugin declaration: %s", configAfter)
	}
}

func TestActionPluginPlanRejectsInvalidLegacyPluginState(t *testing.T) {
	wasm, err := os.ReadFile("../plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", wasm)
	client, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		config string
		lock   string
	}{
		{
			name: "missing lock",
			config: "policy: {require-job-timeout: false}\nplugins:\n" +
				"  - name: " + testutil.FixturePluginName + "\n    version: v1.0.0\n    registry: " + testutil.FixtureRegistryURL + "\n",
		},
		{name: "corrupt lock", config: "policy: {require-job-timeout: false}\n", lock: "not a lock\n"},
		{
			name: "stale lock",
			config: "policy: {require-job-timeout: false}\nplugins:\n" +
				"  - name: " + testutil.FixturePluginName + "\n    version: v2.0.0\n    registry: " + testutil.FixtureRegistryURL + "\n",
			lock: "valid",
		},
		{name: "canonical undeclared lock", config: "lint: {}\n", lock: "valid"},
		{name: "malformed config", config: "policy: [\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolatePluginCache(t)
			workspace := workspaceWith(t, map[string]string{
				".git": "", ".github/actionlint.yaml": tc.config,
			})
			if tc.lock != "" {
				lockPath := filepath.Join(workspace, ".github", pluginregistry.LockName)
				if tc.lock == "valid" {
					lock, err := pluginregistry.OpenLock(t.Context(), lockPath)
					if err != nil {
						t.Fatal(err)
					}
					if err := lock.Set(pin); err != nil {
						t.Fatal(err)
					}
					if err := lock.Save(t.Context()); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(lockPath, []byte(tc.lock), 0600); err != nil {
					t.Fatal(err)
				}
			}
			env := map[string]string{"GITHUB_WORKSPACE": workspace, "INPUT_DISABLE-TOOLS": "shellcheck,ruff"}
			var stdout, stderr bytes.Buffer
			if code := PluginPlan(func(key string) string { return env[key] }, &stdout, &stderr); code != 3 {
				t.Fatalf("got status %d, want failure: %s", code, &stderr)
			}
			if stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("got stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
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
	registry.SetFile(url, wasm)
	workspace := workspaceWith(t, map[string]string{
		"ci.yml": "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo echo test\n",
	})
	env := map[string]string{
		"GITHUB_WORKSPACE": workspace, "INPUT_FILES": "ci.yml", "INPUT_DISABLE-TOOLS": "shellcheck,ruff",
		"INPUT_CONFIG": fmt.Sprintf("plugins: [%q]\n", url+"#sha256="+digest),
	}
	lookup := func(key string) string { return env[key] }
	var stdout, stderr bytes.Buffer
	if code := PluginPlan(lookup, &stdout, &stderr); code != 0 || len(registry.Requests()) != 0 {
		t.Fatalf("inline plan fetched: %d %s %v", code, &stderr, registry.Requests())
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
	if len(registry.Requests()) != 1 || registry.Requests()[0] != url {
		t.Fatalf("inline plugin fetches: %v", registry.Requests())
	}
	registry.ResetRequests()
	registry.SetOffline(true)
	result := runLinter(req)
	if result.code != 1 || len(result.diagnostics) != 1 || result.diagnostics[0].Rule != "no-sudo" || len(registry.Requests()) != 0 {
		t.Fatalf("inline cached plugin failed: %+v %s requests=%v", result.diagnostics, result.stderr, registry.Requests())
	}
}
