package application

import (
	"context"
	"path/filepath"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// configRecorder is a custom rule that keeps the configuration the
// analysis passes to SetConfig.
type configRecorder struct {
	workflowvisit.Base
	config *repoconfig.Config
}

func newConfigRecorder() *configRecorder {
	return &configRecorder{Base: workflowvisit.NewBase("custom-config", "")}
}

func (r *configRecorder) SetConfig(config *repoconfig.Config) { r.config = config }

func TestRulePresetsPreserveAbsentConfiguration(t *testing.T) {
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
		wantNil bool
	}{
		{"default", nil, repoconfig.Presets{}, true},
		{"explicit configuration", &repoconfig.Config{}, repoconfig.Presets{}, false},
		{"strict", nil, repoconfig.Presets{Strict: true}, false},
		{"experimental enabled", nil, repoconfig.Presets{Experimental: new(true)}, false},
		{"experimental disabled", nil, repoconfig.Presets{Experimental: new(false)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := newConfigRecorder()
			called := false
			_, err := runTestAnalysis(context.Background(), analysisRequest{
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
			if !called || (rule.config == nil) != tc.wantNil {
				t.Fatalf("custom rule config = %#v, want nil=%v; hook called=%v", rule.config, tc.wantNil, called)
			}
			if tc.presets.Strict && !rule.config.RequiresCommitHash() {
				t.Fatal("strict preset was not applied")
			}
		})
	}
	t.Run("legacy linter", func(t *testing.T) {
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
		if !called || rule.config != nil {
			t.Fatalf("custom rule config = %#v, want nil; hook called=%v", rule.config, called)
		}
	})
}
