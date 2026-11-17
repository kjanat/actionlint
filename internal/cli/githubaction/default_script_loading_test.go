package githubaction

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

func TestDefaultRuffLoadsOnlyForPython(t *testing.T) {
	for _, tc := range []struct {
		shell             string
		python, composite bool
	}{
		{shell: ""},
		{shell: "bash"},
		{shell: "sh"},
		{shell: "powershell"},
		{shell: "pwsh"},
		{shell: "node -e {0}"},
		{shell: "python -c {0}", python: true},
		{shell: "python", python: true},
		{shell: "python3", python: true},
		{shell: "python -u {0}", python: true},
		{shell: "python", python: true, composite: true},
	} {
		t.Run(fmt.Sprintf("%s/composite=%t", tc.shell, tc.composite), func(t *testing.T) {
			step := "      - run: print(missing)\n"
			if tc.shell != "" {
				step += "        shell: '" + tc.shell + "'\n"
			}
			files := map[string]string{".git": ""}
			if tc.composite {
				files["action/action.yml"] = "name: Python\ndescription: Python\nruns:\n  using: composite\n  steps:\n" + step
				step = "      - uses: ./action\n"
			}
			files["ci.yml"] = "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n" + step
			root := workspaceWith(t, files)
			registry := fixtureregistry.NewPluginRegistry(t)
			addRuffFixture(t, registry)
			acquisition := pluginacquisition.Options{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
			env := map[string]string{"GITHUB_WORKSPACE": root, "INPUT_FILES": "ci.yml", "INPUT_DISABLE-TOOLS": "shellcheck"}
			req, err := provisioningRequest(func(key string) string { return env[key] }, acquisition)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", t.TempDir())
			result := runLinter(req)
			if result.code == 3 {
				t.Fatalf("analysis could not complete: %s", result.stderr)
			}
			found := false
			for _, finding := range result.report.Diagnostics {
				found = found || finding.Rule == "ruff" && finding.Code == "F821"
			}
			if found != tc.python {
				t.Fatalf("Ruff finding=%t, want %t: %+v", found, tc.python, result.report.Diagnostics)
			}
			requests := len(registry.Requests())
			for _, request := range registry.Requests() {
				if !tc.python && strings.Contains(request, "/releases/download/") {
					t.Fatalf("non-Python workflow downloaded Ruff: %v", registry.Requests())
				}
			}
			registry.SetOffline(true)
			req.ctx, _ = network.WithSession(t.Context(), network.Options{Offline: true})
			cached := runLinter(req)
			if cached.code != result.code || len(registry.Requests()) != requests {
				t.Fatalf("offline reuse changed result or made requests: result=%+v requests=%v", cached.lintOutcome, registry.Requests())
			}
		})
	}
}

func addRuffFixture(t *testing.T, registry *fixtureregistry.PluginRegistry) {
	t.Helper()
	module, err := os.ReadFile("../../plugins/pluginexecution/module/testdata/engines/echo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	module, err = testutil.ReplaceWasmDataString(module, []byte(`"engine":{"name":"echo"`), []byte(`"engine":{"name":"ruff"`))
	if err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(module, []byte(`{"metadata_version":1`))
	if start < 0 {
		t.Fatal("echo fixture has no runtime metadata")
	}
	var metadata json.RawMessage
	if err := json.NewDecoder(bytes.NewReader(module[start:])).Decode(&metadata); err != nil {
		t.Fatal(err)
	}
	registry.AddPlugin(t, fixtureregistry.PluginPublisher{PluginID: "actionlint-engine-echo", BaseURL: pluginregistry.DefaultURL, Name: "github.com/kjanat/actionlint", Aliases: []string{"ruff"}, RepositoryID: 99, Metadata: metadata}, "v0.1.0", module)
	if strings.Contains(string(metadata), "template-effects") {
		t.Fatal("fixture unexpectedly declares template support")
	}
}

func TestDefaultRuffComponentUsesDeclaredActionInputs(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	module, err := os.ReadFile("../../plugins/pluginexecution/component/testdata/plugins/ruff-action-input.wasm")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := os.ReadFile("../../plugins/pluginexecution/component/testdata/sources/script-engine/ruff-action-input.metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	var declaration map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &declaration); err != nil {
		t.Fatal(err)
	}
	declaration["repository"] = json.RawMessage(`"https://github.com/publisher/ruff"`)
	metadata, err = json.Marshal(declaration)
	if err != nil {
		t.Fatal(err)
	}
	registry.AddPlugin(t, fixtureregistry.PluginPublisher{PluginID: "echo-component", BaseURL: pluginregistry.DefaultURL, Name: "github.com/publisher/ruff", Aliases: []string{"ruff"}, RepositoryID: 99, Metadata: metadata}, "v1.0.0", module)
	root := workspaceWith(t, map[string]string{
		".git": "", "bash.yml": "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n",
		"input.yml": "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/execute@v1\n        with:\n          code: missing()\n",
	})
	acquisition := pluginacquisition.Options{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	env := map[string]string{"GITHUB_WORKSPACE": root, "INPUT_FILES": "bash.yml", "INPUT_DISABLE-TOOLS": "shellcheck"}
	req, err := provisioningRequest(func(key string) string { return env[key] }, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	result := runLinter(req)
	if result.code != 0 {
		t.Fatalf("Bash workflow failed: %s", result.stderr)
	}
	for _, request := range registry.Requests() {
		if strings.Contains(request, "/releases/download/") {
			t.Fatalf("Bash workflow downloaded component: %v", registry.Requests())
		}
	}
	env["INPUT_FILES"] = "input.yml"
	req, err = provisioningRequest(func(key string) string { return env[key] }, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	result = runLinter(req)
	if result.code != 1 || len(result.report.Diagnostics) != 1 || result.report.Diagnostics[0].Rule != "ruff" || result.report.Diagnostics[0].Code != "ECHO001" {
		t.Fatalf("action input finding: %+v; %s", result.report.Diagnostics, result.stderr)
	}
	downloads := 0
	for _, request := range registry.Requests() {
		if strings.Contains(request, "/releases/download/") {
			downloads++
		}
	}
	if downloads != 1 {
		t.Fatalf("action input downloaded component %d times: %v", downloads, registry.Requests())
	}
	requests := len(registry.Requests())
	registry.SetOffline(true)
	req.ctx, _ = network.WithSession(t.Context(), network.Options{Offline: true})
	cached := runLinter(req)
	if cached.code != result.code || len(cached.report.Diagnostics) != 1 || cached.report.Diagnostics[0].Code != "ECHO001" || len(registry.Requests()) != requests {
		t.Fatalf("offline component result: %+v; %s; requests=%v", cached.report.Diagnostics, cached.stderr, registry.Requests())
	}
}
