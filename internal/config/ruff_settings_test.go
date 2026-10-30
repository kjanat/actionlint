package config_test

import (
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
)

func TestRuffConfiguration(t *testing.T) {
	for _, text := range []string{
		"tools: {ruff: false}",
		"tools: {ruff: {enabled: false, target-version: py312, select: [F, B], ignore: [F401]}}",
		"tools: {ruff: {select: []}}",
		"tools: {ruff: {ignore: []}}",
	} {
		cfg, err := repoconfig.Parse([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		data, err := codec.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		again, err := repoconfig.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		// Flat select and ignore fold into lint.select and lint.ignore.
		before, _ := cfg.Tools["ruff"].Config.Inline()["lint"].(map[string]any)
		after, _ := again.Tools["ruff"].Config.Inline()["lint"].(map[string]any)
		if (before == nil) != (after == nil) {
			t.Fatal("lint settings changed in a round trip")
		}
		if before["select"] != nil && after["select"] == nil {
			t.Fatal("empty selection was lost")
		}
		if before["select"] == nil && after["select"] != nil {
			t.Fatal("default selection became empty selection")
		}
		if (before["ignore"] == nil) != (after["ignore"] == nil) {
			t.Fatal("ignore selection changed between omitted and explicit empty")
		}
	}
	for _, text := range []string{
		"tools: {ruff: {typo: true}}",
		"tools: {ruff: {enabled: wrong}}",
		"tools: {ruff: {enabled: 'false'}}",
		"tools: {ruff: {target-version: []}}",
		"tools: {ruff: {select: {F: true}}}",
		"tools: {ruff: {ignore: ['']}}",
	} {
		if _, err := repoconfig.Parse([]byte(text)); err == nil {
			t.Fatalf("accepted %s", text)
		}
	}
}

func TestRuffTargetVersionConfiguration(t *testing.T) {
	for _, version := range []string{"py37", "py38", "py39", "py310", "py311", "py312", "py313", "py314", "py315", "py27", "py30", "py36", "py316", "py399", "py0314", "3.14", "PY314"} {
		t.Run(version, func(t *testing.T) {
			setting := "ruff: {target-version: '" + version + "'}"
			for _, text := range []string{"tools: {" + setting + "}", "overrides: [{includes: ['**'], tools: {" + setting + "}}]"} {
				_, err := repoconfig.Parse([]byte(text))
				if err != nil {
					t.Fatalf("ParseConfig(%q): %v", text, err)
				}
			}
			if _, err := repoconfig.ParseOverlay("tools", []byte(setting)); err != nil {
				t.Fatalf("ParseConfigOverlay(%q): %v", setting, err)
			}
		})
	}
}

func TestRuffSelectorConfiguration(t *testing.T) {
	for _, tc := range []struct {
		selector string
		valid    bool
	}{
		{"F", true},
		{"F8", true},
		{"F82", true},
		{"F821", true},
		{"ALL", true},
		{"C", true},
		{"T", true},
		{"C9", true},
		{"SIM111", true},
		{"U004", true},
		{"TCH", true},
		{"E111", true},
		{"PLR0904", true},
		{"RUF055", true},
		{"CPY001", true},
		{"ANN101", true},
		{"ANN102", true},
		{"S320", true},
		{"PGH001", true},
		{"XYZ", true},
		{"F9999", true},
		{"F999", true},
		{"P", true},
		{"RUF940", true},
		{"PREVIEW", true},
		{"undefined-name", true},
		{"correctness", true},
		{"", false},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			for _, key := range []string{"select", "ignore"} {
				setting := "ruff: {" + key + ": ['" + tc.selector + "']}"
				for _, text := range []string{"tools: {" + setting + "}", "overrides: [{includes: ['**'], tools: {" + setting + "}}]"} {
					_, err := repoconfig.Parse([]byte(text))
					if (err == nil) != tc.valid || (err != nil && !strings.Contains(err.Error(), "tools.ruff.config: at /lint/"+key+"/0: minLength")) {
						t.Fatalf("ParseConfig(%q): %v; valid=%v", text, err, tc.valid)
					}
				}
				if _, err := repoconfig.ParseOverlay("tools", []byte(setting)); (err == nil) != tc.valid {
					t.Fatalf("ParseConfigOverlay(%q): %v; valid=%v", setting, err, tc.valid)
				}
			}
		})
	}
}
