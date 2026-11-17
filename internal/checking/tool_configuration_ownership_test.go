package checking_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
)

type configurationNoopRunner struct{}

func (configurationNoopRunner) Analyze(context.Context, tools.Batch) ([]tools.ToolFinding, error) {
	return nil, nil
}

func TestToolRequirementsCannotMutateSiblingConfiguration(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "ci.yml")
	configPath := filepath.Join(root, "actionlint.yaml")
	for name, content := range map[string]string{path: "on: push\njobs: {}\n", configPath: "lint:\n  enabled: true\n"} {
		if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	seen := 0
	providers := []tools.Registration{
		{Descriptor: rule.Descriptor{Name: "mutator", Category: "correctness"}, NeedsProcess: func(_ context.Context, cfg, project *configuration.Config) (bool, error) {
			cfg.Lint.Enabled, project.Lint.Enabled = new(false), new(false)
			return false, nil
		}},
		{Descriptor: rule.Descriptor{Name: "observer", Category: "correctness"}, NeedsProcess: func(_ context.Context, cfg, project *configuration.Config) (bool, error) {
			seen++
			if cfg.Lint.Enabled == nil || !*cfg.Lint.Enabled || project.Lint.Enabled == nil || !*project.Lint.Enabled {
				t.Error("tool requirement callback mutated another provider's configuration")
			}
			return false, nil
		}},
	}
	if _, err := checking.RequiredTools(t.Context(), checking.Options{WorkingDir: root, ConfigFile: configPath, Tools: providers}, []string{path}); err != nil {
		t.Fatal(err)
	}
	if seen == 0 {
		t.Fatal("tool requirements were not evaluated")
	}
}

func TestToolCallbacksCannotMutateHostConfiguration(t *testing.T) {
	t.Parallel()
	for _, callback := range []string{"enabled", "build", "required", "input selectors"} {
		t.Run(callback, func(t *testing.T) {
			t.Parallel()
			cfg, err := checking.ConfigurationRules().Parse([]byte("lint:\n  enabled: true\n"))
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			mutate := func(c tools.Context) {
				calls++
				for _, selected := range []*configuration.Config{c.Config, c.ProjectConfig} {
					if selected != nil {
						selected.Lint.Enabled = new(false)
					}
				}
			}
			provider := tools.Registration{
				Descriptor: rule.Descriptor{Name: "ownership-test", Category: "correctness"},
				Scope:      rule.SourceConfiguration, SupportsActions: true,
				Build: func(c tools.Context) (tools.Runner, error) {
					if callback == "build" {
						mutate(c)
					}
					if callback == "required" {
						return nil, errors.New("optional tool unavailable")
					}
					return configurationNoopRunner{}, nil
				},
				Enabled: func(c tools.Context) bool {
					if callback == "enabled" {
						mutate(c)
					}
					return true
				},
				Required: func(c tools.Context) bool { mutate(c); return false },
				InputSelectors: func(c tools.Context) ([]checkinputs.ScriptInput, error) {
					if callback == "input selectors" {
						mutate(c)
					}
					return nil, nil
				},
			}
			root := t.TempDir()
			options := checking.Options{WorkingDir: root, Tools: []tools.Registration{provider}}
			options.Runners.AsOf, _ = lifecycle.ParseDate("2026-11-01")
			result, err := checking.Analyze(t.Context(), options, []workspace.InputFile{{
				Path: filepath.Join(root, "ci.yml"), Config: cfg,
				Content: []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-node@v4\n        with:\n          node-version: '16'\n      - run: echo safe\n"),
			}})
			if err != nil {
				t.Fatal(err)
			}
			if calls == 0 {
				t.Fatal("the configured callback was not exercised")
			}
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "runtime-lifecycle" {
					return
				}
			}
			t.Fatalf("tool callback changed host publication: %+v", result.Diagnostics)
		})
	}
}
