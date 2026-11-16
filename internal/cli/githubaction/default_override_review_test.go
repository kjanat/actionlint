package githubaction

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
)

func TestDefaultRuffEnabledByWorkflowOverride(t *testing.T) {
	registry := fixtureregistry.NewPluginRegistry(t)
	addRuffFixture(t, registry)
	root := workspaceWith(t, map[string]string{
		".git":                   "",
		".github/actionlint.yml": "tools: {ruff: false}\noverrides:\n  - includes: ['python.yml']\n    tools: {ruff: true}\n",
		"python.yml":             "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\n",
		"bash.yml":               "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n",
	})
	acquisition := pluginacquisition.Settings{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	env := map[string]string{"GITHUB_WORKSPACE": root, "INPUT_FILES": "python.yml", "INPUT_DISABLE-TOOLS": "shellcheck"}
	req, err := provisioningRequest(func(key string) string { return env[key] }, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	result := runLinter(req)
	if result.code != 1 || len(result.report.Diagnostics) != 1 || result.report.Diagnostics[0].Rule != "ruff" || result.report.Diagnostics[0].Code != "F821" {
		t.Fatalf("override-enabled Ruff finding: %+v; %s", result.report.Diagnostics, result.stderr)
	}
	env["INPUT_FILES"] = "bash.yml"
	req, err = provisioningRequest(func(key string) string { return env[key] }, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	result = runLinter(req)
	if result.code != 0 {
		t.Fatalf("disabled Ruff workflow failed: %s", result.stderr)
	}
}
