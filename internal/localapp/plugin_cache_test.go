package localapp

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/config"
	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
)

// Restoring one digest must satisfy every reference to those bytes, including
// candidates and per-file tool overrides. Planning must work before a cache exists.
func TestCachePlanCombinesCandidateAndToolArtifacts(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("LocalAppData", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	a, b, c := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	backend := func(path string) *toolconfig.Backend {
		module, err := toolconfig.NewModule(path)
		if err != nil {
			t.Fatal(err)
		}
		return toolconfig.WasmBackend(module)
	}
	tools := config.ToolsConfig{
		"ruff":       {Backend: backend("sha256://" + b)},
		"shellcheck": {Backend: backend("https://127.0.0.1:1/shell.wasm#sha256=" + c)},
	}
	sources := PluginSources{
		Paths: []string{"sha256://" + a, "sha256://" + a, "https://127.0.0.1:1/plugin.wasm#sha256=" + strings.ToUpper(a), "local.wasm"},
		configs: []*config.Config{{
			Tools: tools,
			Overrides: []config.Override{{Tools: &config.ToolsConfig{
				"ruff": {Backend: backend("sha256://" + c)},
			}}},
		}},
	}
	plan, err := sources.CachePlan()
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Directory != filepath.Join(base, "actionlint", "artifacts") || !reflect.DeepEqual(plan.Artifacts, []string{a, b, c}) {
		t.Fatalf("cache restore plan: %+v", plan)
	}
	if _, err := os.Stat(plan.Directory); !os.IsNotExist(err) {
		t.Fatalf("planning acquired artifacts or created a cache: %v", err)
	}
}

// Invalid candidate locations must fail before a restore key can refer to
// different bytes or interpret URI credentials, paths or fragments as a digest.
func TestCachePlanRejectsMalformedCandidateSources(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, source := range []string{
		"sha256://", "sha256://short", "sha256://" + strings.Repeat("g", 64),
		"sha256://" + strings.ToUpper(digest), "sha256://user@" + digest,
		"sha256://" + digest + "/file", "sha256://" + digest + "?query",
		"sha256://" + digest + "?", "sha256://" + digest + "#fragment",
	} {
		t.Run(source, func(t *testing.T) {
			sources := PluginSources{Paths: []string{source}}
			if _, err := sources.CachePlan(); err == nil {
				t.Fatalf("invalid candidate entered cache plan: %q", source)
			}
		})
	}
}
