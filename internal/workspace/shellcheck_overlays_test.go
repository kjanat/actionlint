package workspace_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
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
			path := writeToolFixture(t, t.TempDir(), "actionlint.yml", tc.base)
			overlay, err := repoconfig.ParseOverlay("tools", []byte(tc.overlay))
			if err != nil {
				t.Fatal(err)
			}
			var report repoconfig.Report
			session, err := workspace.New(t.Context(), workspace.Options{ConfigFile: path, ConfigOverlays: []repoconfig.Overlay{overlay}, OnConfigLoaded: func(value repoconfig.Report) { report = value }})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := session.ConfigForProject(nil)
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
	session, err := workspace.New(t.Context(), workspace.Options{ConfigFile: path, ConfigOverlays: []repoconfig.Overlay{overlay}, OnConfigLoaded: func(value repoconfig.Report) { report = value }})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := session.ConfigForProject(nil)
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
	if err := codec.NewEncoder(&encoded).Encode(values); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(encoded.String(), "enabled: true") {
		t.Fatalf("default not resolved: %s", encoded.String())
	}
}
