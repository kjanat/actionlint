package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
)

func TestToolBooleanShorthand(t *testing.T) {
	for _, tc := range []struct {
		text, name string
		enabled    bool
		additional []string
	}{
		{"tools: {ruff: false}", "ruff", false, nil},
		{"tools: {shellcheck: true}", "shellcheck", true, nil},
		{"tools: {lint-sql: false}", "lint-sql", false, []string{"lint-sql"}},
		{"overrides: [{includes: ['**'], tools: {lint-sql: false}}]", "lint-sql", false, []string{"lint-sql"}},
	} {
		cfg, err := repoconfig.Parse([]byte(tc.text), tc.additional...)
		if err != nil {
			t.Fatalf("%s: %v", tc.text, err)
		}
		tools := cfg.Tools
		if len(cfg.Overrides) != 0 {
			effective, err := repoconfig.ForFile(cfg, "action.yml", "")
			if err != nil {
				t.Fatal(err)
			}
			tools = effective.Tools
		}
		tool, ok := tools[tc.name]
		if !ok || tool.Enabled == nil || *tool.Enabled != tc.enabled || tool.Backend != nil || tool.Config != nil {
			t.Fatalf("%s: %+v", tc.text, tools)
		}
	}
}

func TestToolShorthandOverridesOnlyEnabled(t *testing.T) {
	cfg, err := repoconfig.Parse([]byte(`tools:
  lint-sql: {config: {dialect: postgres}}
overrides:
  - includes: ['**']
    tools: {lint-sql: false}
`), "lint-sql")
	if err != nil {
		t.Fatal(err)
	}
	effective, err := repoconfig.ForFile(cfg, "action.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	tool := effective.Tools["lint-sql"]
	if tool.Enabled == nil || *tool.Enabled || !reflect.DeepEqual(tool.Config.Inline(), map[string]any{"dialect": "postgres"}) {
		t.Fatalf("shorthand replaced more than enabled: %+v", tool)
	}
}

func TestFlatRuffKeysFoldIntoConfig(t *testing.T) {
	cfg, err := repoconfig.Parse([]byte(`tools:
  ruff:
    target-version: py312
    select: [F, B]
    ignore: []
    config: {lint: {extend-select: [S]}}
`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"target-version": "py312",
		"lint":           map[string]any{"extend-select": []any{"S"}, "select": []any{"F", "B"}, "ignore": []any{}},
	}
	if got := cfg.Tools["ruff"].Config.Inline(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	encoded, err := codec.Marshal(cfg.Tools)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "\n    select:") || !strings.Contains(string(encoded), "config:") {
		t.Fatalf("folded keys must serialize under config:\n%s", encoded)
	}
}

// A flat key and the property it sets may both be written: the shorthand's
// declared precedence keeps flat select and ignore and the configured
// target-version, as the separate settings did before folding. Ruff's own
// top-level select stays a native option.
func TestFlatRuffKeysPrecedence(t *testing.T) {
	for _, tc := range []struct {
		text string
		want map[string]any
	}{
		{
			"tools: {ruff: {select: [E], config: {lint: {select: [F], extend-select: [B]}}}}",
			map[string]any{"lint": map[string]any{"select": []any{"E"}, "extend-select": []any{"B"}}},
		},
		{
			"tools: {ruff: {target-version: py311, config: {target-version: py312}}}",
			map[string]any{"target-version": "py312"},
		},
		{
			"tools: {ruff: {target-version: py311, config: {target-version: null}}}",
			map[string]any{"target-version": "py311"},
		},
		{
			"tools: {ruff: {select: null, config: {lint: {select: [F]}}}}",
			map[string]any{"lint": map[string]any{"select": []any{"F"}}},
		},
		{
			"tools: {ruff: {ignore: [F401], config: {lint: null}}}",
			map[string]any{"lint": map[string]any{"ignore": []any{"F401"}}},
		},
		{
			"tools: {ruff: {config: {select: [E], lint: {select: [F]}}}}",
			map[string]any{"select": []any{"E"}, "lint": map[string]any{"select": []any{"F"}}},
		},
	} {
		cfg, err := repoconfig.Parse([]byte(tc.text))
		if err != nil {
			t.Fatalf("%s: %v", tc.text, err)
		}
		if got := cfg.Tools["ruff"].Config.Inline(); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: got %#v, want %#v", tc.text, got, tc.want)
		}
	}
	for text, want := range map[string]string{
		"tools: {ruff: {select: [F], config: {lint: true}}}": "config.lint must be a mapping",
		"tools: {ruff: {select: [F], config: ./ruff.toml}}":  "cannot be combined with a configuration file path",
	} {
		if _, err := repoconfig.Parse([]byte(text)); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v, want %q", text, err, want)
		}
	}
}

func TestFlatRuffKeysMergeAcrossOverrides(t *testing.T) {
	cfg, err := repoconfig.Parse([]byte(`tools:
  ruff: {select: [F], target-version: py312}
overrides:
  - includes: ['**']
    tools: {ruff: {ignore: [F401], target-version: null}}
`))
	if err != nil {
		t.Fatal(err)
	}
	effective, err := repoconfig.ForFile(cfg, "action.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"lint": map[string]any{"select": []any{"F"}, "ignore": []any{"F401"}}, "target-version": nil}
	if got := effective.Tools["ruff"].Config.Inline(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestToolConfigPathRequiresConfigFiles(t *testing.T) {
	if _, err := repoconfig.Parse([]byte("tools: {ruff: {config: ./ruff.toml}}")); err == nil || !strings.Contains(err.Error(), "reads no configuration files") {
		t.Fatalf("Ruff reads no configuration files, got %v", err)
	}
	for _, text := range []string{"tools: {shellcheck: {config: ./.shellcheckrc}}", "tools: {lint-sql: {config: ./sql.toml}}"} {
		if _, err := repoconfig.Parse([]byte(text), "lint-sql"); err != nil {
			t.Fatalf("%s: %v", text, err)
		}
	}
}

func TestUnknownToolNames(t *testing.T) {
	for _, text := range []string{
		"tools: {pyflakes: {enabled: false}}",
		"tools: {expression: false}",
		"overrides: [{includes: ['**'], tools: {pyflakes: false}}]",
	} {
		if _, err := repoconfig.Parse([]byte(text)); err == nil || !strings.Contains(err.Error(), "unknown tool") {
			t.Fatalf("%s: %v", text, err)
		}
		// Loading binds names to registered rules of any kind; the composition
		// layer then binds them to tool providers only.
		cfg, err := repoconfig.Parse([]byte(text), "pyflakes", "expression")
		if err != nil {
			t.Fatalf("registered rule rejected while loading %s: %v", text, err)
		}
		if err := repoconfig.ValidateToolNames(cfg); err == nil || !strings.Contains(err.Error(), "unknown tool") {
			t.Fatalf("%s: analyzer name accepted as a tool: %v", text, err)
		}
		if err := repoconfig.ValidateToolNames(cfg, "pyflakes", "expression"); err != nil {
			t.Fatalf("registered provider rejected in %s: %v", text, err)
		}
	}
	if err := repoconfig.ValidateToolNames(&repoconfig.Config{Tools: repoconfig.ToolsConfig{"ruff": {}, "shellcheck": {}}}); err != nil {
		t.Fatalf("bundled providers need no registration: %v", err)
	}
	// Input structure is validated before plugins load; names bind afterwards.
	overlay, err := repoconfig.ParseOverlayBeforePlugins("tools", []byte("{pyflakes: false}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repoconfig.Overlaid(nil, nil, []repoconfig.Overlay{overlay}); err == nil {
		t.Fatal("unregistered overlay tool accepted after binding")
	}
	if _, _, err := repoconfig.Overlaid(nil, nil, []repoconfig.Overlay{overlay}, "pyflakes"); err != nil {
		t.Fatal(err)
	}
}

func TestRegisteredToolEntriesAreStructural(t *testing.T) {
	cfg, err := repoconfig.Parse([]byte("tools: {lint-sql: {enabled: true, backend: {wasm: {path: sql.wasm}}, config: {anything: [1, 2]}}}"), "lint-sql")
	if err != nil {
		t.Fatal(err)
	}
	tool := cfg.Tools["lint-sql"]
	if tool.Backend == nil || tool.Backend.Wasm == nil || !reflect.DeepEqual(tool.Config.Inline(), map[string]any{"anything": []any{1, 2}}) {
		t.Fatalf("lost settings: %+v", tool)
	}
	// Providers without bundled data have no shorthands to fold.
	for _, text := range []string{
		"tools: {lint-sql: {dialect: postgres}}",
		"tools: {lint-sql: {enabled: 'false'}}",
		"tools: {lint-sql: [true]}",
		"tools: {lint-sql: {config: 1}}",
	} {
		if _, err := repoconfig.Parse([]byte(text), "lint-sql"); err == nil {
			t.Fatalf("accepted %s", text)
		}
	}
}

func TestInheritedToolConfigPathsResolveFromDeclaringFile(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "base")
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		filepath.Join(base, "actionlint.yaml"): "tools:\n  lint-sql: {config: ./sql.toml}\noverrides:\n  - includes: ['**']\n    tools: {shellcheck: {config: ./.shellcheckrc}}\n",
		filepath.Join(root, "actionlint.yaml"): "extends: [base/actionlint.yaml]\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := repoconfig.Load(filepath.Join(root, "actionlint.yaml"), "lint-sql")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.Tools["lint-sql"].Config.Path(), filepath.Join(base, "sql.toml"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	effective, err := repoconfig.ForFile(cfg, "action.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := effective.Tools["shellcheck"].Config.Path(), filepath.Join(base, ".shellcheckrc"); got != want {
		t.Fatalf("override path: got %q, want %q", got, want)
	}
}

func TestEffectiveValuesEnableBundledTools(t *testing.T) {
	cfg, err := repoconfig.Parse([]byte("tools: {lint-sql: {config: {dialect: postgres}}}"), "lint-sql")
	if err != nil {
		t.Fatal(err)
	}
	values, err := repoconfig.EffectiveValues(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tools, ok := values["tools"].(map[string]any)
	if !ok {
		t.Fatalf("tools: %#v", values["tools"])
	}
	for _, name := range []string{"ruff", "shellcheck"} {
		if tool, ok := tools[name].(map[string]any); !ok || tool["enabled"] != true {
			t.Fatalf("%s default: %#v", name, tools[name])
		}
	}
	if tool, ok := tools["lint-sql"].(map[string]any); !ok || tool["enabled"] != nil {
		t.Fatalf("registered tools keep omission: %#v", tools["lint-sql"])
	}
	if _, set := cfg.Tools["ruff"]; set {
		t.Fatal("effective defaults mutated the configuration")
	}
}
