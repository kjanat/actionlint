package consumer_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/analysis"
	"actionlint.kjanat.dev/v2/config"
)

const workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n"

func TestValidatedRuleOptions(t *testing.T) {
	for _, tc := range []struct {
		name, action, rule string
		exclude            []string
	}{
		{"required action", "actions/upload-artifact", "required-actions", []string{"actions/checkout"}},
		{"commit pin", "actions/checkout", "require-commit-hash", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exclusions, err := json.Marshal(tc.exclude)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Parse([]byte(fmt.Sprintf(`lint: {rules: {preset: none, policy: {required-actions: {level: error, options: {actions: [%q]}}, require-commit-hash: {level: error, options: {exclude: %s}}}}}`, tc.action, exclusions)))
			if err != nil {
				t.Fatal(err)
			}
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

func TestShellcheckSourcesAreValidatedAndOwned(t *testing.T) {
	for _, settings := range []string{"'./.shellcheckrc'", "{disable: [SC2086], enable: [all], shell: bash, extended-analysis: true, external-sources: false, source-path: [scripts]}"} {
		source := []byte("tools: {shellcheck: {config: " + settings + "}}")
		parsed, err := config.Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		before, err := parsed.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		clear(source)
		after, err := parsed.MarshalText()
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("mutable source retained: %v", err)
		}
	}
	for _, settings := range []string{`""`, `"bad\npath"`, `"bad\u0000path"`, `{disable: ["SC2086\nenable=all"]}`, `{enable: [some_check]}`, `{shell: python}`, `{source-path: ['']}`, `{typo: true}`} {
		if _, err := config.Parse([]byte("tools: {shellcheck: {config: " + settings + "}}")); err == nil {
			t.Errorf("accepted invalid settings %s", settings)
		}
	}
	if _, err := config.Parse([]byte("tools: {plugin-tool: {config: {level: 2}}}"), "plugin-tool"); err != nil {
		t.Fatal(err)
	}
}
