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

	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func pluginCacheDirectory(userCacheDir string) string {
	return filepath.Join(userCacheDir, "actionlint", "artifacts")
}

func TestActionExactRegistryPlugins(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                 string
		explicit, legacyLock bool
	}{
		{name: "discovered"},
		{name: "explicit", explicit: true},
		{name: "discovered-legacy-lock", legacyLock: true},
		{name: "explicit-legacy-lock", explicit: true, legacyLock: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			wasm, err := os.ReadFile("../../plugins/execution/component/testdata/plugins/no-sudo.wasm")
			if err != nil {
				t.Fatal(err)
			}
			registry := fixtureregistry.NewPluginRegistry(t)
			acquisition := plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
			directory := pluginCacheDirectory(registry.UserCacheDir())
			registry.Add(t, "v1.0.0", wasm)
			client, err := plugininstall.New(fixtureregistry.FixtureRegistryURL, acquisition)
			if err != nil {
				t.Fatal(err)
			}
			pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
			if err != nil {
				t.Fatal(err)
			}
			workspace := workspaceWith(t, map[string]string{
				".git": "", "ci.yml": "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo echo test\npermissions: {}\n",
				".github/base.yaml":       "plugins:\n  - name: " + fixtureregistry.FixturePluginName + "\n    version: v1.0.0\n    registry: " + fixtureregistry.FixtureRegistryURL + "\n",
				".github/actionlint.yaml": "extends: [base.yaml]\n",
			})
			lockPath := filepath.Join(workspace, ".github", plugininstall.LockName)
			if test.legacyLock {
				lock, err := plugininstall.OpenLock(t.Context(), lockPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := lock.Set(pin); err != nil {
					t.Fatal(err)
				}
				if err := lock.Save(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			env := map[string]string{
				"GITHUB_WORKSPACE": workspace, "INPUT_FILES": "ci.yml", "INPUT_DISABLE-TOOLS": "shellcheck,ruff",
				"INPUT_CONFIG": "lint: {rules: {plugins: {no-sudo: warn}}}",
			}
			if test.explicit {
				env["INPUT_CONFIG-FILE"] = ".github/actionlint.yaml"
			}
			lookup := func(key string) string { return env[key] }
			registry.ResetRequests()
			registry.SetOffline(true)
			var stdout, stderr bytes.Buffer
			if code := PluginPlan(lookup, &stdout, &stderr, acquisition); code != 0 {
				t.Fatalf("plugin plan %d: %s", code, &stderr)
			}
			var plan plugininstall.CachePlan
			if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
				t.Fatal(err)
			}
			if plan.Directory != directory || len(plan.Artifacts) != 1 || plan.Artifacts[0] != pin.SHA256 || len(registry.Requests()) != 0 {
				t.Fatalf("plan=%+v requests=%v", plan, registry.Requests())
			}
			registry.SetOffline(false)
			req, err := provisioningRequest(lookup, acquisition)
			if err != nil {
				t.Fatal(err)
			}
			cold := runLinter(req)
			if cold.code != 1 || len(cold.report.Diagnostics) != 1 || cold.report.Diagnostics[0].Rule != "no-sudo" {
				t.Fatalf("cold component result: %+v, %s", cold.report.Diagnostics, cold.stderr)
			}
			if len(registry.Requests()) != 1 || registry.Requests()[0] != pin.URL {
				t.Fatalf("analysis resolved registry or fetched repeatedly: %v", registry.Requests())
			}
			registry.ResetRequests()
			registry.SetOffline(true)
			result := runLinter(req)
			if result.code != 1 || len(result.report.Diagnostics) != 1 || result.report.Diagnostics[0].Rule != "no-sudo" {
				t.Fatalf("locked component result: %+v, %s", result.report.Diagnostics, result.stderr)
			}
			if len(registry.Requests()) != 0 {
				t.Fatalf("cached Action fetched artifacts: %v", registry.Requests())
			}
			if !test.legacyLock {
				if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
					t.Fatalf("checking created a lockfile: %v", err)
				}
			}
			env["INPUT_CONFIG"] = "plugins: []"
			req, err = provisioningRequest(lookup, acquisition)
			if err != nil {
				t.Fatal(err)
			}
			result = runLinter(req)
			if result.code != 0 || len(result.report.Diagnostics) != 0 {
				t.Fatalf("inline plugin reset ignored: %+v %s", result.report.Diagnostics, result.stderr)
			}
		})
	}
}

func TestActionConfiguredWasm(t *testing.T) {
	for _, fixture := range []string{"../../plugins/execution/module/testdata/engines/echo.wasm", "../../plugins/execution/component/testdata/plugins/ruff-action-input.wasm"} {
		t.Run(fixture, func(t *testing.T) {
			acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir()}
			module, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(fixture, "/module/") {
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
			workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\npermissions: {}\n"
			if strings.Contains(fixture, "/component/") {
				workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/execute@v1\n        with:\n          code: missing();\npermissions: {}\n"
			}
			workspace := workspaceWith(t, map[string]string{
				".git": "", "ci.yml": workflow,
				".github/actionlint.yaml": "tools:\n  ruff:\n    backend:\n      wasm:\n        path: " + string(encoded) + "\n",
			})
			t.Setenv("PATH", t.TempDir())
			env := map[string]string{"GITHUB_WORKSPACE": workspace, "INPUT_FILES": "ci.yml", "INPUT_DISABLE-TOOLS": "shellcheck"}
			lookup := func(key string) string { return env[key] }
			req, err := provisioningRequest(lookup, acquisition)
			if err != nil {
				t.Fatal(err)
			}
			result := runLinter(req)
			wantCode := "ECHO001"
			if strings.Contains(fixture, "/module/") {
				wantCode = "F821"
			}
			if result.code != 1 || len(result.report.Diagnostics) != 1 || result.report.Diagnostics[0].Code != wantCode {
				t.Fatalf("configured Wasm result: %+v %s", result.report.Diagnostics, result.stderr)
			}
			env["INPUT_DISABLE-TOOLS"] = "shellcheck,ruff"
			req, err = provisioningRequest(lookup, acquisition)
			if err != nil {
				t.Fatal(err)
			}
			result = runLinter(req)
			if result.code != 0 {
				t.Fatalf("disabled engine ran: %+v %s", result.report.Diagnostics, result.stderr)
			}
		})
	}
}

func TestActionEnabledRuffDoesNotRequireNativeExecutable(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(strconv.FormatBool(configured), func(t *testing.T) {
			files := map[string]string{
				".git":   "",
				"ci.yml": "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\npermissions: {}\n",
			}
			if configured {
				files[".github/actionlint.yaml"] = "tools: {ruff: {backend: {process: {executable: missing-ruff}}}}\n"
			}
			workspace := workspaceWith(t, files)
			t.Setenv("PATH", t.TempDir())
			env := map[string]string{"GITHUB_WORKSPACE": workspace, "INPUT_FILES": "ci.yml", "INPUT_DISABLE-TOOLS": "shellcheck"}
			req, err := provisioningRequest(func(key string) string { return env[key] }, plugininstall.Acquisition{})
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
	t.Parallel()
	acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir()}
	directory := pluginCacheDirectory(acquisition.UserCacheDir)
	digest := strings.Repeat("a", 64)
	workspace := workspaceWith(t, map[string]string{
		".git": "", ".github/actionlint.yaml": "tools:\n  ruff:\n    backend:\n      wasm:\n        path: https://example.invalid/ruff.wasm#sha256=" + digest + "\n",
	})
	env := map[string]string{"GITHUB_WORKSPACE": workspace}
	var stdout, stderr bytes.Buffer
	if code := PluginPlan(func(key string) string { return env[key] }, &stdout, &stderr, acquisition); code != 0 {
		t.Fatalf("plan failed: %d %s", code, &stderr)
	}
	var plan plugininstall.CachePlan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Directory != directory || len(plan.Artifacts) != 1 || plan.Artifacts[0] != digest {
		t.Fatalf("configured digest missing: %+v", plan)
	}
}

func TestActionRejectsUndeclaredLegacyLock(t *testing.T) {
	t.Parallel()
	wasm, err := os.ReadFile("../../plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry := fixtureregistry.NewPluginRegistry(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	registry.Add(t, "v1.0.0", wasm)
	client, err := plugininstall.New(fixtureregistry.FixtureRegistryURL, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaceWith(t, map[string]string{
		".git":                    "",
		".github/actionlint.yaml": "policy: {require-job-timeout: false}\n",
		"ci.yml":                  "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo echo test\npermissions: {}\n",
	})
	lockPath := filepath.Join(workspace, ".github", plugininstall.LockName)
	lock, err := plugininstall.OpenLock(t.Context(), lockPath)
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
	if code := PluginPlan(lookup, &stdout, &stderr, acquisition); code != 3 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "not declared") {
		t.Fatalf("cache plan accepted undeclared lock: %d %s", code, &stderr)
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

	req, err := provisioningRequest(lookup, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	result := runLinter(req)
	if result.code != 3 || len(result.report.Diagnostics) != 0 || !strings.Contains(result.stderr, "not declared") {
		t.Fatalf("checking accepted undeclared lock: %+v %s", result.report.Diagnostics, result.stderr)
	}
	if len(registry.Requests()) != 0 {
		t.Fatalf("checking fetched undeclared plugin bytes: %v", registry.Requests())
	}
	configAfter, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	lockAfter, err = os.ReadFile(lockPath)
	if err != nil || !bytes.Equal(configBefore, configAfter) || !bytes.Equal(lockBefore, lockAfter) {
		t.Fatal("checking rewrote legacy config or lock")
	}
}

func TestActionPluginPlanRejectsInvalidLegacyPluginState(t *testing.T) {
	t.Parallel()
	wasm, err := os.ReadFile("../../plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", wasm)
	client, err := plugininstall.New(fixtureregistry.FixtureRegistryURL, plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()})
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		config string
		lock   string
	}{
		{name: "corrupt lock", config: "policy: {require-job-timeout: false}\n", lock: "not a lock\n"},
		{
			name: "stale lock",
			config: "policy: {require-job-timeout: false}\nplugins:\n" +
				"  - name: " + fixtureregistry.FixturePluginName + "\n    version: v2.0.0\n    registry: " + fixtureregistry.FixtureRegistryURL + "\n",
			lock: "valid",
		},
		{name: "canonical undeclared lock", config: "lint: {}\n", lock: "valid"},
		{name: "malformed config", config: "policy: [\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir(), Connections: registry.Connections()}
			workspace := workspaceWith(t, map[string]string{
				".git": "", ".github/actionlint.yaml": tc.config,
			})
			if tc.lock != "" {
				lockPath := filepath.Join(workspace, ".github", plugininstall.LockName)
				if tc.lock == "valid" {
					lock, err := plugininstall.OpenLock(t.Context(), lockPath)
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
			if code := PluginPlan(func(key string) string { return env[key] }, &stdout, &stderr, acquisition); code != 3 {
				t.Fatalf("got status %d, want failure: %s", code, &stderr)
			}
			if stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("got stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestActionInlinePinnedPlugin(t *testing.T) {
	t.Parallel()
	wasm, err := os.ReadFile("../../plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	url := "https://plugins.example/no-sudo.wasm"
	digest := fmt.Sprintf("%x", sha256.Sum256(wasm))
	registry := fixtureregistry.NewPluginRegistry(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	registry.SetFile(url, wasm)
	workspace := workspaceWith(t, map[string]string{
		"ci.yml": "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo echo test\npermissions: {}\n",
	})
	env := map[string]string{
		"GITHUB_WORKSPACE": workspace, "INPUT_FILES": "ci.yml", "INPUT_DISABLE-TOOLS": "shellcheck,ruff",
		"INPUT_CONFIG": fmt.Sprintf("plugins: [%q]\n", url+"#sha256="+digest),
	}
	lookup := func(key string) string { return env[key] }
	var stdout, stderr bytes.Buffer
	if code := PluginPlan(lookup, &stdout, &stderr, acquisition); code != 0 || len(registry.Requests()) != 0 {
		t.Fatalf("inline plan fetched: %d %s %v", code, &stderr, registry.Requests())
	}
	if !strings.Contains(stdout.String(), digest) {
		t.Fatalf("inline digest missing: %s", &stdout)
	}
	req, err := provisioningRequest(lookup, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	cold := runLinter(req)
	if cold.code != 1 || len(cold.report.Diagnostics) != 1 || cold.report.Diagnostics[0].Rule != "no-sudo" {
		t.Fatalf("inline cold plugin failed: %+v %s", cold.report.Diagnostics, cold.stderr)
	}
	if len(registry.Requests()) != 1 || registry.Requests()[0] != url {
		t.Fatalf("inline plugin fetches: %v", registry.Requests())
	}
	registry.ResetRequests()
	registry.SetOffline(true)
	result := runLinter(req)
	if result.code != 1 || len(result.report.Diagnostics) != 1 || result.report.Diagnostics[0].Rule != "no-sudo" || len(registry.Requests()) != 0 {
		t.Fatalf("inline cached plugin failed: %+v %s requests=%v", result.report.Diagnostics, result.stderr, registry.Requests())
	}
}
