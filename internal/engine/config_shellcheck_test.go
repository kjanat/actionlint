package engine

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/yaml"
)

func TestShellcheckShorthandOverlay(t *testing.T) {
	for _, tc := range []struct {
		name, base, overlay string
		enabled             bool
	}{
		{"turn off retaining config", `tools:
  shellcheck:
    enabled: true
    config: ./.shellcheckrc
`, "shellcheck: false", false},
		{"add config retaining disabled", `tools:
  shellcheck: false
`, "shellcheck: {config: ./.shellcheckrc}", false},
		{"merged shorthand", `tools:
  <<: {shellcheck: false}
`, "shellcheck: {config: ./.shellcheckrc}", false},
		{"enable retaining config", `tools:
  shellcheck:
    enabled: false
    config: ./.shellcheckrc
`, "shellcheck: true", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeShellcheckFixture(t, t.TempDir(), "actionlint.yml", tc.base)
			overlay, err := repoconfig.ParseOverlay("tools", []byte(tc.overlay))
			if err != nil {
				t.Fatal(err)
			}
			var report repoconfig.Report
			session, err := NewAnalysisSession(AnalysisOptions{ConfigFile: path, ConfigOverlays: []repoconfig.Overlay{overlay}, OnConfigLoaded: func(value repoconfig.Report) { report = value }})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := session.configForProject(nil)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Tools.Shellcheck.Enabled == nil || *cfg.Tools.Shellcheck.Enabled != tc.enabled || cfg.Tools.Shellcheck.Config.Path() != "./.shellcheckrc" || cfg.Path() != path {
				t.Fatalf("lost enabled, config or config file: %+v", cfg)
			}
			origin := report.Inspection.Origins["/tools/shellcheck/enabled"]
			if strings.HasSuffix(tc.overlay, "true") || strings.HasSuffix(tc.overlay, "false") {
				if origin.Source != "input" || origin.Input != "tools" {
					t.Fatalf("shorthand input provenance lost: %+v", origin)
				}
			} else if origin.Source != "config" || origin.Line != 2 {
				t.Fatalf("shorthand file provenance lost: %+v", origin)
			}
		})
	}
}

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
			linter, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: command, WorkingDir: root})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, ".github/workflows/test.yml")
			if err := os.WriteFile(path, []byte(workflow), 0o600); err != nil {
				t.Fatal(err)
			}
			findings, err := linter.LintFile(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			var found []*findingmodel.Finding
			for _, finding := range findings {
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

func TestShellcheckConfigOverlayOrigins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actionlint.yaml")
	if err := os.WriteFile(path, []byte(`tools:
  shellcheck:
    enabled: false
    config:
      disable: [SC2086]
      enable: [all]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	overlay, err := repoconfig.ParseOverlay("tools", []byte("shellcheck: {enabled: null, config: {disable: []}}"))
	if err != nil {
		t.Fatal(err)
	}
	var report repoconfig.Report
	session, err := NewAnalysisSession(AnalysisOptions{ConfigFile: path, ConfigOverlays: []repoconfig.Overlay{overlay}, OnConfigLoaded: func(value repoconfig.Report) { report = value }})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := session.configForProject(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Shellcheck.Enabled != nil || len(cfg.Tools.Shellcheck.Config.Inline().Disable) != 0 || len(cfg.Tools.Shellcheck.Config.Inline().Enable) != 1 {
		t.Fatalf("incorrect overlay: %+v", cfg.Tools.Shellcheck)
	}
	for _, pointer := range []string{"/tools/shellcheck/enabled", "/tools/shellcheck/config/disable"} {
		origin := report.Inspection.Origins[pointer]
		if origin.Source != "input" || origin.Input != "tools" {
			t.Fatalf("lost input provenance at %s: %+v", pointer, origin)
		}
	}
	if origin := report.Inspection.Origins["/tools/shellcheck/config/enable"]; origin.Source != "config" {
		t.Fatalf("lost file provenance: %+v", origin)
	}
	values, err := repoconfig.EffectiveValues(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var encoded strings.Builder
	if err := yaml.NewEncoder(&encoded).Encode(values); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(encoded.String(), "enabled: true") {
		t.Fatalf("default not resolved: %s", encoded.String())
	}
}
