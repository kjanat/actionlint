package workspace_test

import (
	"reflect"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestRuffTargetVersionEmptyAndReset(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"''", false},
		{`""`, false},
		{"null", true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			setting := "ruff: {target-version: " + tc.value + "}"
			for _, text := range []string{"tools: {" + setting + "}", "overrides: [{includes: ['**'], tools: {" + setting + "}}]"} {
				_, err := repoconfig.Parse([]byte(text))
				if (err == nil) != tc.valid || (err != nil && !strings.Contains(err.Error(), "tools.ruff.config: at /target-version")) {
					t.Fatalf("ParseConfig(%q): %v; valid=%v", text, err, tc.valid)
				}
			}
			if _, err := repoconfig.ParseOverlay("tools", []byte(setting)); (err == nil) != tc.valid {
				t.Fatalf("ParseConfigOverlay(%q): %v; valid=%v", setting, err, tc.valid)
			}
		})
	}
	for _, overlayText := range []string{"ruff: {}", "ruff: {target-version: null}"} {
		path := writeToolFixture(t, t.TempDir(), "actionlint.yml", "tools: {ruff: {target-version: py312}}")
		overlay, err := repoconfig.ParseOverlay("tools", []byte(overlayText))
		if err != nil {
			t.Fatal(err)
		}
		session, err := workspace.New(t.Context(), workspace.Options{ConfigFile: path, ConfigOverlays: []repoconfig.Overlay{overlay}})
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := session.ConfigForProject(nil)
		if err != nil {
			t.Fatal(err)
		}
		resolved, err := repoconfig.EffectiveValues(cfg)
		if err != nil {
			t.Fatal(err)
		}
		var want any = "py312"
		if strings.Contains(overlayText, "null") {
			want = nil
		}
		// The flat target-version key is folded into the tool's config.
		if got := resolved["tools"].(map[string]any)["ruff"].(map[string]any)["config"].(map[string]any)["target-version"]; got != want {
			t.Fatalf("overlay %q resolved to %v, want %v", overlayText, got, want)
		}
	}
}

func TestRuffShorthandOverlay(t *testing.T) {
	for _, tc := range []struct {
		name, base, overlay string
		enabled             bool
	}{
		{"disable retains settings", "tools: {ruff: {target-version: py312, select: [F821], ignore: [F401]}}", "ruff: false", false},
		{"enable retains settings", "tools: {ruff: {enabled: false, target-version: py312, select: [F821], ignore: [F401]}}", "ruff: true", true},
		{"settings retain switch", "tools: {ruff: false}", "ruff: {target-version: py312, select: [F821], ignore: [F401]}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeToolFixture(t, t.TempDir(), "actionlint.yml", tc.base)
			overlay, err := repoconfig.ParseOverlay("tools", []byte(tc.overlay))
			if err != nil {
				t.Fatal(err)
			}
			session, err := workspace.New(t.Context(), workspace.Options{ConfigFile: path, ConfigOverlays: []repoconfig.Overlay{overlay}})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := session.ConfigForProject(nil)
			if err != nil {
				t.Fatal(err)
			}
			ruff := cfg.Tools["ruff"]
			settings := ruff.Config.Inline()
			want := map[string]any{"target-version": "py312", "lint": map[string]any{"select": []any{"F821"}, "ignore": []any{"F401"}}}
			if ruff.Enabled == nil || *ruff.Enabled != tc.enabled || !reflect.DeepEqual(settings, want) {
				t.Fatalf("lost Ruff settings: %+v", ruff)
			}
		})
	}
}
