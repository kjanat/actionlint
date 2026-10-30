package shellcheck

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/config"
)

func TestShellcheckOptionalCheckShorthand(t *testing.T) {
	for _, value := range []string{"all", "true", "True", "TRUE", "[all]", "'all'", "&all all", "{<<: {enable: true}}"} {
		settings := "{enable: " + value + "}"
		if strings.HasPrefix(value, "{") {
			settings = value
		}
		cfg, err := config.Parse([]byte("tools: {shellcheck: {enabled: false, config: " + settings + "}}"))
		if err != nil {
			t.Fatalf("%s: %v", value, err)
		}
		tool := cfg.Tools["shellcheck"]
		if tool.Enabled == nil || *tool.Enabled {
			t.Fatalf("%s enabled tool", value)
		}
		inline, err := inlineConfig(cfg)
		if err != nil {
			t.Fatalf("%s: %v", value, err)
		}
		if !slices.Equal(inline.Enable, []string{"all"}) {
			t.Fatalf("%s: %v", value, inline.Enable)
		}
		directives, err := inline.Directives()
		if err != nil || directives != "# shellcheck enable=all\n" {
			t.Fatalf("%s: %q, %v", value, directives, err)
		}
	}
	for _, value := range []string{"false", "'true'", "quote-safe-variables", "42"} {
		if _, err := config.Parse([]byte("tools: {shellcheck: {config: {enable: " + value + "}}}")); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
}

func TestShellcheckShorthandFormatsAndInheritance(t *testing.T) {
	write := func(t *testing.T, dir, name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for filename, source := range map[string]string{
		"actionlint.yaml": "tools: {shellcheck: {config: {enable: true}}}",
		"actionlint.json": `{"tools":{"shellcheck":{"config":{"enable":"all"}}}}`,
		"actionlint.toml": "tools.shellcheck.config.enable = true",
	} {
		t.Run(filename, func(t *testing.T) {
			dir := t.TempDir()
			base := write(t, dir, filename, source)
			cfg, err := config.Load(base)
			if err != nil {
				t.Fatal(err)
			}
			if inline, err := inlineConfig(cfg); err != nil || !slices.Equal(inline.Enable, []string{"all"}) {
				t.Fatalf("%s: %+v, %v", filename, cfg, err)
			}
			child := write(t, dir, "child.yaml", "extends: ["+filename+"]\ntools: {shellcheck: {config: {enable: []}}}")
			cfg, err = config.Load(child)
			if err != nil {
				t.Fatal(err)
			}
			if inline, err := inlineConfig(cfg); err != nil || len(inline.Enable) != 0 {
				t.Fatalf("list did not clear inherited checks: %+v, %v", cfg, err)
			}
		})
	}
}
