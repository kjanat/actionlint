package consumer_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/analysis"
	"actionlint.kjanat.dev/v2/config"
)

const workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n"

func TestTypedRuleOptions(t *testing.T) {
	for _, tc := range []struct {
		name, action, rule string
		exclude            []string
	}{
		{"required action", "actions/upload-artifact", "required-actions", []string{"actions/checkout"}},
		{"commit pin", "actions/checkout", "require-commit-hash", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Lint: config.LintConfig{Rules: config.LintRulesConfig{
				Preset: "none",
				Policy: config.RuleGroupConfig{Rules: map[string]config.RuleSetting{
					"required-actions":    {Level: "error", Options: &config.RequiredActionsOptions{Actions: []string{tc.action}}},
					"require-commit-hash": {Level: "error", Options: &config.CommitHashOptions{Exclude: tc.exclude}},
				}},
			}}}
			result, err := analysis.Analyze(t.Context(), analysis.Request{
				Sources: []analysis.Source{{Path: "workflow.yml", Content: []byte(workflow), Config: cfg}},
				Options: analysis.Options{WorkingDir: t.TempDir()},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != tc.rule {
				t.Fatalf("typed rule options ignored: %#v", result.Diagnostics)
			}
		})
	}
}

func TestOverlayErrorsAreClassifiable(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.CommandContext(t.Context(), "git", "init", "--quiet", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	path := filepath.Join(root, ".github", "workflows", "ci.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}
	var overlays []config.Overlay
	for _, source := range []string{"{require-job-timeout: {min-minutes: 20}}", "{require-job-timeout: {max-minutes: 10}}"} {
		overlay, err := config.ParseOverlay("policy", []byte(source))
		if err != nil {
			t.Fatal(err)
		}
		overlays = append(overlays, overlay)
	}
	options := analysis.FileOptions{Options: analysis.Options{WorkingDir: root}, SkipProjectConfig: true, ConfigOverlays: overlays}
	for _, mode := range []string{"files", "repository"} {
		t.Run(mode, func(t *testing.T) {
			var err error
			if mode == "files" {
				_, err = analysis.Files(t.Context(), []string{path}, options)
			} else {
				_, err = analysis.Repository(t.Context(), root, options)
			}
			var overlayError *config.OverlayError
			if !errors.As(err, &overlayError) || errors.Unwrap(overlayError) == nil {
				t.Fatalf("cannot classify merged overlay validation: %v", err)
			}
		})
	}
}

func TestShellcheckSourcesCanBeConstructed(t *testing.T) {
	file, err := config.ToolConfigPath("./.shellcheckrc")
	if err != nil || file.Path() != "./.shellcheckrc" || file.Inline() != nil {
		t.Fatalf("path source: %#v: %v", file, err)
	}
	disable, enable, sourcePath := []string{"SC2086"}, []string{"all"}, []string{"scripts"}
	settings := map[string]any{"disable": disable, "enable": enable, "shell": "bash", "extended-analysis": true, "external-sources": false, "source-path": sourcePath}
	inline, err := config.ToolConfigInline("shellcheck", settings)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := config.Parse([]byte("tools: {shellcheck: {config: {disable: [SC2086], enable: [all], shell: bash, extended-analysis: true, external-sources: false, source-path: [scripts]}}}"))
	if err != nil || !reflect.DeepEqual(inline.Inline(), parsed.Tools["shellcheck"].Config.Inline()) || inline.Path() != "" {
		t.Fatalf("inline constructor differs from parsed settings: %#v: %v", inline, err)
	}
	disable[0], enable[0], sourcePath[0] = "SC2154", "check-set-e-suppressed", "other"
	settings["shell"], settings["extended-analysis"] = "sh", false
	if !reflect.DeepEqual(inline.Inline(), parsed.Tools["shellcheck"].Config.Inline()) {
		t.Fatal("constructor stored mutable caller settings")
	}
	for _, path := range []string{"", "bad\npath", "bad\x00path"} {
		if _, err := config.ToolConfigPath(path); err == nil {
			t.Errorf("accepted invalid rc path %q", path)
		}
	}
	for _, settings := range []map[string]any{
		{"disable": []string{"SC2086\nenable=all"}}, {"enable": []string{"some_check"}}, {"shell": "python"}, {"source-path": []string{""}}, {"typo": true},
	} {
		if _, err := config.ToolConfigInline("shellcheck", settings); err == nil {
			t.Errorf("accepted invalid inline settings %#v", settings)
		}
	}
	if _, err := config.ToolConfigInline("plugin-tool", map[string]any{"level": 2}); err != nil {
		t.Errorf("a plugin's provider validates its own settings: %v", err)
	}
}
