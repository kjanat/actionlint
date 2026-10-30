package application_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
)

func TestShellcheckInlineConfigAnalysis(t *testing.T) {
	command, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("ShellCheck required")
	}
	t.Setenv("SHELLCHECK_OPTS", "")
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
			root := t.TempDir()
			t.Chdir(root)
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
			result, err := application.Check(t.Context(), application.Options{WorkingDir: root, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command}})}, application.Input{Kind: application.Files, Paths: []string{path}})
			if err != nil {
				t.Fatal(err)
			}
			var found []diagnostic.Diagnostic
			for _, finding := range result.Diagnostics {
				if finding.Rule == "shellcheck" {
					found = append(found, finding)
				}
			}
			if tc.finding == "" && len(found) == 0 {
				return
			}
			line := 7 + strings.Count(tc.script, "\n")
			if tc.finding == "" || len(found) != 1 || !strings.Contains(found[0].Message, tc.finding) || found[0].Start.Line != line {
				t.Fatalf("want %q on workflow line %d; got %v", tc.finding, line, found)
			}
		})
	}
}
