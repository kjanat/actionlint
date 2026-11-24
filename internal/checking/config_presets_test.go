package checking

import (
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

type configRecorder struct {
	workflowvisit.Base
	config *configuration.Config
}

func newConfigRecorder() *configRecorder {
	return &configRecorder{Base: workflowvisit.NewBase("custom-config", "")}
}

func (r *configRecorder) SetConfig(config *configuration.Config) { r.config = config }

func TestRulePresetsPrepareConfigurationWithoutFile(t *testing.T) {
	t.Parallel()
	content := []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`)
	for _, tc := range []struct {
		name    string
		config  *configuration.Config
		presets configuration.Presets
	}{
		{"default", nil, configuration.Presets{}},
		{"explicit configuration", &configuration.Config{}, configuration.Presets{}},
		{"strict", nil, configuration.Presets{Strict: true}},
		{"experimental enabled", nil, configuration.Presets{Experimental: new(true)}},
		{"experimental disabled", nil, configuration.Presets{Experimental: new(false)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rule := newConfigRecorder()
			called := false
			_, err := runTestAnalysis(t.Context(), analysisRequest{
				Sources:     []workspace.InputFile{{Path: "test.yml", Content: content, Config: tc.config}},
				WorkingDir:  t.TempDir(),
				RulePresets: tc.presets,
				OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
					called = true
					return append(rules, rule)
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !called || rule.config == nil {
				t.Fatalf("prepared configuration was not supplied; hook called=%v", called)
			}
			if _, resolved := rule.config.Lint.Rules.ResolvedLevel("syntax-check"); !resolved {
				t.Fatal("rule received unresolved defaults")
			}
			if tc.presets.Strict && !rule.config.RequiresCommitHash() {
				t.Fatal("strict preset was not applied")
			}
		})
	}
	t.Run("without project", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		rule := newConfigRecorder()
		called := false
		request := analysisRequest{WorkingDir: root, OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
			called = true
			return append(rules, rule)
		}, Sources: []workspace.InputFile{{Path: filepath.Join(root, "test.yml"), InputPath: filepath.Join(root, "test.yml"), Content: content, Project: nil}}}
		if _, err := testFindings(runTestAnalysis(t.Context(), request)); err != nil {
			t.Fatal(err)
		}
		if !called || rule.config == nil {
			t.Fatalf("prepared configuration was not supplied; hook called=%v", called)
		}
		if _, resolved := rule.config.Lint.Rules.ResolvedLevel("syntax-check"); !resolved {
			t.Fatal("rule received unresolved defaults outside a project")
		}
	})
}
