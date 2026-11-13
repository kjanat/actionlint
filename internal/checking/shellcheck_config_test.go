package checking_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestShellcheckInlineConfigAnalysis(t *testing.T) {
	t.Parallel()
	command, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("ShellCheck required")
	}
	for _, tc := range []struct {
		name, settings, script, finding string
	}{
		{"disabled", "enabled: false", "echo $VALUE", ""},
		{"enabled", "enabled: true", "echo $VALUE", "SC2086"},
		{"code disabled", "config: {disable: [SC2086]}", "echo $VALUE", ""},
		{"range disabled", "config: {disable: [SC2000-SC3000]}", "echo $VALUE", ""},
		{"dialect override", "config: {shell: sh}", `[[ -n "$HOME" ]]`, "SC3010"},
		{"safe variable default", "enabled: true", "value=hello\necho $value", ""},
		{"optional safe variable quoting", "config: {enable: [quote-safe-variables]}", "value=hello\necho $value", "SC2248"},
		{"multiple prefix lines", "config: {disable: [SC2016], enable: [quote-safe-variables], extended-analysis: false}", "echo $VALUE", "SC2086"},
		{"source path", `config: {source-path: ["lib's directory"], external-sources: true}`, ". config.sh\necho $VALUE", ""},
		{"external sources disabled", `config: {source-path: ["lib's directory"], external-sources: false}`, ". config.sh\necho $VALUE", "SC2086"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for _, dir := range []string{".git", ".github/workflows", "lib's directory"} {
				if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for name, content := range map[string]string{
				".github/actionlint.yaml": `tools:
  shellcheck:
    ` + tc.settings + "\n",
				"lib's directory/config.sh": "VALUE=42\n",
			} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			workflow := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: |
          ` + strings.ReplaceAll(tc.script, "\n", "\n          ") + "\n"
			path := filepath.Join(root, ".github/workflows/test.yml")
			if err := os.WriteFile(path, []byte(workflow), 0o600); err != nil {
				t.Fatal(err)
			}
			result, err := checking.Check(t.Context(), checking.Options{WorkingDir: root, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command}})}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{path}})
			if err != nil {
				t.Fatal(err)
			}
			var found []results.Diagnostic
			for _, finding := range result.Diagnostics {
				if finding.Rule == "shellcheck" {
					found = append(found, finding)
				}
			}
			if tc.finding == "" && len(found) == 0 {
				return
			}
			line := 7 + strings.Count(tc.script, "\n")
			if tc.finding == "" || len(found) != 1 || found[0].Code != tc.finding || found[0].Start.Line != line {
				t.Fatalf("want %q on workflow line %d; got %v", tc.finding, line, found)
			}
		})
	}
}
