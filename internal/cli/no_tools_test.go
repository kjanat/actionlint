package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestNoToolsKeepsBuiltinChecks(t *testing.T) {
	t.Parallel()
	workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo echo '${{ missing.value }}'\npermissions: {}\n"
	for _, args := range [][]string{
		{"check", "--no-config", "--no-tools"},
		{"--no-tools", "check", "--no-config", "--tool", "ruff=missing-native-tool"},
		{"check", "--no-config", "--no-tools", "--plugin", "internal/plugins/execution/module/testdata/engines/echo.wasm"},
		{"check", "--no-config", "--no-tools", "--plugin", "internal/plugins/execution/component/testdata/plugins/no-sudo.wasm"},
		{"check", "--no-config", "--no-tools", "--plugin", "missing-tool-provider.json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir()}
			status, output, log := runOfflineCommand(t, acquisition, workflow, append(args, "--json", "-")...)
			var report struct{ Diagnostics []struct{ Rule string } }
			if err := json.Unmarshal([]byte(output), &report); err != nil || status != 1 || len(report.Diagnostics) == 0 {
				t.Fatalf("status=%d output=%s log=%s error=%v", status, output, log, err)
			}
			for _, finding := range report.Diagnostics {
				if finding.Rule != "expression-reference" {
					t.Errorf("unexpected diagnostic from %s: %s", finding.Rule, output)
				}
			}
		})
	}
}

func TestNoToolsOverridesConfiguredBackends(t *testing.T) {
	t.Parallel()
	for _, backend := range []string{
		"process: {executable: missing-native-tool}",
		"wasm: {path: missing-engine.wasm}",
	} {
		t.Run(backend, func(t *testing.T) {
			t.Parallel()
			config := filepath.Join(t.TempDir(), "actionlint.yaml")
			if err := os.WriteFile(config, []byte("plugins: []\ntools: {ruff: {backend: {"+backend+"}}}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print(missing)\npermissions: {}\n"
			status, output, log := runOfflineCommand(t, plugininstall.Acquisition{UserCacheDir: t.TempDir()}, workflow, "check", "--config", config, "--no-tools", "--json", "-")
			if status != 0 {
				t.Fatalf("disabled backend ran: status=%d output=%s log=%s", status, output, log)
			}
		})
	}
}
