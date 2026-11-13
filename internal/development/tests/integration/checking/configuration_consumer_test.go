package checking_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

const configurationWorkflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n"

func TestValidatedRuleOptions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		exclude []string
		want    int
	}{
		{"commit pin", nil, 1},
		{"excluded action", []string{"actions/checkout"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			exclusions, err := json.Marshal(tc.exclude)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := checking.ConfigurationRules().Parse([]byte(fmt.Sprintf(`lint: {rules: {preset: none, policy: {require-commit-hash: {level: error, options: {exclude: %s}}}}}`, exclusions)))
			if err != nil {
				t.Fatal(err)
			}
			result, err := checking.Analyze(t.Context(),
				checking.Options{WorkingDir: t.TempDir()},
				[]workspace.Source{{Path: "workflow.yml", Content: []byte(configurationWorkflow), Config: cfg}},
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != tc.want {
				t.Fatalf("typed rule options ignored: %#v", result.Diagnostics)
			}
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule != "unpinned-reference" {
					t.Fatalf("unexpected rule: %#v", diagnostic)
				}
			}
		})
	}
}

func TestOverlayErrorsAreClassifiable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if output, err := exec.CommandContext(t.Context(), "git", "init", "--quiet", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	path := filepath.Join(root, ".github", "workflows", "ci.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(configurationWorkflow), 0o600); err != nil {
		t.Fatal(err)
	}
	var overlays []config.Overlay
	for _, source := range []string{"{require-job-timeout: {min-minutes: 20}}", "{require-job-timeout: {max-minutes: 10}}"} {
		overlay, err := checking.ConfigurationRules().ParseOverlay("policy", []byte(source))
		if err != nil {
			t.Fatal(err)
		}
		overlays = append(overlays, overlay)
	}
	options := checking.Options{WorkingDir: root, SkipProjectConfig: true, ConfigOverlays: overlays}
	for _, mode := range []string{"files", "repository"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var err error
			if mode == "files" {
				_, err = checking.Check(t.Context(), options, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{path}})
			} else {
				_, err = checking.Check(t.Context(), options, inputmodel.Request{Kind: inputmodel.Repository, Directory: root})
			}
			var overlayError *config.OverlayError
			if !errors.As(err, &overlayError) || errors.Unwrap(overlayError) == nil {
				t.Fatalf("cannot classify merged overlay validation: %v", err)
			}
		})
	}
}

func TestShellcheckSourcesAreValidatedAndOwned(t *testing.T) {
	t.Parallel()
	for _, settings := range []string{"'./.shellcheckrc'", "{disable: [SC2086], enable: [all], shell: bash, extended-analysis: true, external-sources: false, source-path: [scripts]}"} {
		source := []byte("tools: {shellcheck: {config: " + settings + "}}")
		parsed, err := checking.ConfigurationRules().Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		before, err := checking.ConfigurationRules().Marshal(parsed)
		if err != nil {
			t.Fatal(err)
		}
		clear(source)
		after, err := checking.ConfigurationRules().Marshal(parsed)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("mutable source retained: %v", err)
		}
	}
	for _, settings := range []string{`""`, `"bad\npath"`, `"bad\u0000path"`, `{disable: ["SC2086\nenable=all"]}`, `{enable: [some_check]}`, `{shell: python}`, `{source-path: ['']}`, `{typo: true}`} {
		if _, err := checking.ConfigurationRules().Parse([]byte("tools: {shellcheck: {config: " + settings + "}}")); err == nil {
			t.Errorf("accepted invalid settings %s", settings)
		}
	}
	if _, err := checking.ConfigurationRules().Parse([]byte("tools: {plugin-tool: {config: {level: 2}}}"), "plugin-tool"); err != nil {
		t.Fatal(err)
	}
}
