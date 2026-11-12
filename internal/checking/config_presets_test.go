package checking

import (
	"path/filepath"
	"testing"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

// configRecorder records the prepared configuration supplied to a custom rule.
type configRecorder struct {
	workflowvisit.Base
	config *repoconfig.Config
}

func newConfigRecorder() *configRecorder {
	return &configRecorder{Base: workflowvisit.NewBase("custom-config", "")}
}

func (r *configRecorder) SetConfig(config *repoconfig.Config) { r.config = config }

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
		config  *repoconfig.Config
		presets repoconfig.Presets
	}{
		{"default", nil, repoconfig.Presets{}},
		{"explicit configuration", &repoconfig.Config{}, repoconfig.Presets{}},
		{"strict", nil, repoconfig.Presets{Strict: true}},
		{"experimental enabled", nil, repoconfig.Presets{Experimental: new(true)}},
		{"experimental disabled", nil, repoconfig.Presets{Experimental: new(false)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rule := newConfigRecorder()
			called := false
			_, err := runTestAnalysis(t.Context(), analysisRequest{
				Sources:     []sourceUnit{{Path: "test.yml", Content: content, Config: tc.config}},
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
		}}

		request.Sources = []sourceUnit{{Path: filepath.Join(root, "test.yml"), InputPath: filepath.Join(root, "test.yml"), Content: content, Project: nil}}
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
