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
