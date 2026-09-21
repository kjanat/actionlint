package actionlint

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestShellcheckConfigValidation(t *testing.T) {
	for _, value := range []string{
		`true`, `false`, `{config: './.shellcheckrc'}`,
		`{enabled: false}`, `{enabled: true, config: {}}`,
		`{config: {disable: [SC2086, SC3000-SC4000, all], enable: [all], shell: bash, extended-analysis: false, external-sources: false, source-path: ['my scripts']}}`,
		`{config: null}`, `{enabled: null, config: {shell: null, disable: null}}`,
		`{config: {disable: [&code SC2086, *code]}}`,
		`{config: {<<: {disable: [SC2086]}}}`,
	} {
		if _, err := ParseConfig([]byte("tools: {shellcheck: " + value + "}")); err != nil {
			t.Errorf("%s: %v", value, err)
		}
	}
	for _, value := range []string{
		`{enabled: banana}`, `{config: {format: json}}`, `{config: {severity: warning}}`,
		`{config: {disable: [2086]}}`, `{config: {disable: ['SC2086\nenable=all']}}`,
		`{config: {enable: [some_check]}}`, `{config: {shell: python}}`, `{config: {shell: ''}}`,
		`{config: {extended-analysis: yes}}`, `{config: {source-path: ['']}}`,
		`{config: {source-path: ["bad\npath"]}}`, `{config: ''}`, `{config: {typo: true}}`,
		`{config: {<<: {disable: [2086]}}}`, `{config: {disable: [&code 2086, *code]}}`,
	} {
		if _, err := ParseConfig([]byte("tools: {shellcheck: " + value + "}")); err == nil {
			t.Errorf("accepted %s", value)
		}
	}
}

func TestShellcheckShorthandOverlay(t *testing.T) {
	for _, tc := range []struct {
		name, base, overlay string
		enabled             bool
	}{
		{"turn off retaining config", "tools:\n  shellcheck:\n    enabled: true\n    config: ./.shellcheckrc\n", "shellcheck: false", false},
		{"add config retaining disabled", "tools:\n  shellcheck: false\n", "shellcheck: {config: ./.shellcheckrc}", false},
		{"merged shorthand", "tools:\n  <<: {shellcheck: false}\n", "shellcheck: {config: ./.shellcheckrc}", false},
		{"enable retaining config", "tools:\n  shellcheck:\n    enabled: false\n    config: ./.shellcheckrc\n", "shellcheck: true", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeShellcheckFixture(t, t.TempDir(), "actionlint.yml", tc.base)
			overlay, err := ParseConfigOverlay("tools", []byte(tc.overlay))
			if err != nil {
				t.Fatal(err)
			}
			var report ConfigReport
			session, err := NewAnalysisSession(AnalysisOptions{ConfigFile: path, ConfigOverlays: []ConfigOverlay{overlay}, OnConfigLoaded: func(value ConfigReport) { report = value }})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := session.configForProject(nil)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Tools.Shellcheck.Enabled == nil || *cfg.Tools.Shellcheck.Enabled != tc.enabled || cfg.Tools.Shellcheck.Config.path() != "./.shellcheckrc" || cfg.filename != path {
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
				".github/actionlint.yaml":   "tools:\n  shellcheck:\n    " + tc.settings + "\n",
				"lib's directory/config.sh": "VALUE=42\n",
			} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: |\n          " + strings.ReplaceAll(tc.script, "\n", "\n          ") + "\n"
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
			var found []*Error
			for _, finding := range findings {
				if finding.Kind == "shellcheck" {
					found = append(found, finding)
				}
			}
			if tc.finding == "" && len(found) == 0 {
				return
			}
			line := 7 + strings.Count(tc.script, "\n")
			if tc.finding == "" || len(found) != 1 || !strings.Contains(found[0].Message, tc.finding) || found[0].Line != line {
				t.Fatalf("want %q on workflow line %d; got %v", tc.finding, line, found)
			}
		})
	}
}

func TestShellcheckConfigOverlayOrigins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actionlint.yaml")
	if err := os.WriteFile(path, []byte("tools:\n  shellcheck:\n    enabled: false\n    config:\n      disable: [SC2086]\n      enable: [all]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	overlay, err := ParseConfigOverlay("tools", []byte("shellcheck: {enabled: null, config: {disable: []}}"))
	if err != nil {
		t.Fatal(err)
	}
	var report ConfigReport
	session, err := NewAnalysisSession(AnalysisOptions{ConfigFile: path, ConfigOverlays: []ConfigOverlay{overlay}, OnConfigLoaded: func(value ConfigReport) { report = value }})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := session.configForProject(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Shellcheck.Enabled != nil || len(cfg.Tools.Shellcheck.Config.inline().Disable) != 0 || len(cfg.Tools.Shellcheck.Config.inline().Enable) != 1 {
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
	values, err := effectiveConfig(cfg)
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
