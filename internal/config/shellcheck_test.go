package config

import (
	"slices"
	"strings"
	"testing"
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
		if _, err := Parse([]byte("tools: {shellcheck: " + value + "}")); err != nil {
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
		if _, err := Parse([]byte("tools: {shellcheck: " + value + "}")); err == nil {
			t.Errorf("accepted %s", value)
		}
	}
}

func TestShellcheckOptionalCheckShorthand(t *testing.T) {
	for _, value := range []string{"all", "true", "True", "TRUE", "[all]", "'all'", "&all all", "{<<: {enable: true}}"} {
		settings := "{enable: " + value + "}"
		if strings.HasPrefix(value, "{") {
			settings = value
		}
		cfg, err := Parse([]byte("tools: {shellcheck: {enabled: false, config: " + settings + "}}"))
		if err != nil {
			t.Fatalf("%s: %v", value, err)
		}
		tool := cfg.Tools.Shellcheck
		if tool.Enabled == nil || *tool.Enabled {
			t.Fatalf("%s enabled tool", value)
		}
		inline := tool.Config.Inline()
		if !slices.Equal(inline.Enable, []string{"all"}) {
			t.Fatalf("%s: %v", value, inline.Enable)
		}
		directives, err := inline.Directives()
		if err != nil || directives != "# shellcheck enable=all\n" {
			t.Fatalf("%s: %q, %v", value, directives, err)
		}
	}
	for _, value := range []string{"false", "'true'", "quote-safe-variables", "42"} {
		if _, err := Parse([]byte("tools: {shellcheck: {config: {enable: " + value + "}}}")); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
}

func TestShellcheckShorthandFormatsAndInheritance(t *testing.T) {
	for filename, source := range map[string]string{
		"actionlint.yaml": "tools: {shellcheck: {config: {enable: true}}}",
		"actionlint.json": `{"tools":{"shellcheck":{"config":{"enable":"all"}}}}`,
		"actionlint.toml": "tools.shellcheck.config.enable = true",
	} {
		t.Run(filename, func(t *testing.T) {
			dir := t.TempDir()
			base := writeConfig(t, dir, filename, source)
			cfg, err := Load(base)
			if err != nil || !slices.Equal(cfg.Tools.Shellcheck.Config.Inline().Enable, []string{"all"}) {
				t.Fatalf("%s: %+v, %v", filename, cfg, err)
			}
			child := writeConfig(t, dir, "child.yaml", "extends: ["+filename+"]\ntools: {shellcheck: {config: {enable: []}}}")
			cfg, err = Load(child)
			if err != nil || len(cfg.Tools.Shellcheck.Config.Inline().Enable) != 0 {
				t.Fatalf("list did not clear inherited checks: %+v, %v", cfg, err)
			}
		})
	}
}
