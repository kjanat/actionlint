package config_test

import (
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/yaml"
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
		data, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		again, err := repoconfig.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Tools.Ruff.Select != nil && again.Tools.Ruff.Select == nil {
			t.Fatal("empty selection was lost")
		}
		if cfg.Tools.Ruff.Select == nil && again.Tools.Ruff.Select != nil {
			t.Fatal("default selection became empty selection")
		}
		if (cfg.Tools.Ruff.Ignore == nil) != (again.Tools.Ruff.Ignore == nil) {
			t.Fatal("ignore selection changed between omitted and explicit empty")
		}
	}
	for _, text := range []string{
		"tools: {ruff: {typo: true}}",
		"tools: {ruff: {enabled: wrong}}",
		"tools: {ruff: {enabled: 'false'}}",
		"tools: {ruff: {target-version: '--fix'}}",
		"tools: {ruff: {select: ['F,--fix']}}",
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
			valid := version == "py37" || version == "py38" || version == "py39" || version == "py310" || version == "py311" || version == "py312" || version == "py313" || version == "py314" || version == "py315"
			setting := "ruff: {target-version: '" + version + "'}"
			for _, text := range []string{"tools: {" + setting + "}", "overrides: [{includes: ['**'], tools: {" + setting + "}}]"} {
				_, err := repoconfig.Parse([]byte(text))
				if (err == nil) != valid || (err != nil && !strings.Contains(err.Error(), "tools.ruff.target-version")) {
					t.Fatalf("ParseConfig(%q): %v; valid=%v", text, err, valid)
				}
			}
			if _, err := repoconfig.ParseOverlay("tools", []byte(setting)); (err == nil) != valid || (err != nil && !strings.Contains(err.Error(), "tools.ruff.target-version")) {
				t.Fatalf("ParseConfigOverlay(%q): %v; valid=%v", setting, err, valid)
			}
		})
	}
}

func TestRuffSelectorConfiguration(t *testing.T) {
	for _, tc := range []struct {
		selector string
		valid    bool
	}{
		{"F", true}, {"F8", true}, {"F82", true}, {"F821", true},
		{"ALL", true}, {"C", true}, {"T", true}, {"C9", true},
		{"SIM111", true}, {"U004", true}, {"TCH", true},
		{"E111", false}, {"PLR0904", false}, {"RUF055", false}, {"CPY001", true},
		{"ANN101", false}, {"ANN102", false}, {"S320", false}, {"PGH001", true},
		{"XYZ", false}, {"F9999", false}, {"F999", false},
		{"P", false}, {"RUF940", false}, {"PREVIEW", false},
		{"undefined-name", false}, {"correctness", false}, {"", false},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			for _, key := range []string{"select", "ignore"} {
				setting := "ruff: {" + key + ": ['" + tc.selector + "']}"
				for _, text := range []string{"tools: {" + setting + "}", "overrides: [{includes: ['**'], tools: {" + setting + "}}]"} {
					_, err := repoconfig.Parse([]byte(text))
					if (err == nil) != tc.valid || (err != nil && !strings.Contains(err.Error(), "invalid rule selector")) {
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
