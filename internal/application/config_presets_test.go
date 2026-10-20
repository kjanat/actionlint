package application

import (
	"context"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/builtin"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

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
			rule := struct {
				workflowvisit.Base
				builtin.Configuration
			}{Base: workflowvisit.NewBase("custom-config", "")}
			called := false
			_, err := runTestAnalysis(context.Background(), analysisRequest{
				Sources:     []sourceUnit{{Path: "test.yml", Content: content, Config: tc.config}},
				WorkingDir:  t.TempDir(),
				RulePresets: tc.presets,
				OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
					called = true
					return append(rules, &rule)
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !called || (rule.Config() == nil) != tc.wantNil {
				t.Fatalf("custom rule config = %#v, want nil=%v; hook called=%v", rule.Config(), tc.wantNil, called)
			}
			if tc.presets.Strict && !rule.Config().RequiresCommitHash() {
				t.Fatal("strict preset was not applied")
			}
		})
	}
	t.Run("legacy linter", func(t *testing.T) {
		root := t.TempDir()
		rule := struct {
			workflowvisit.Base
			builtin.Configuration
		}{Base: workflowvisit.NewBase("custom-config", "")}
		called := false
		request := analysisRequest{WorkingDir: root, OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
			called = true
			return append(rules, &rule)
		}}

		request.Sources = []sourceUnit{{Path: filepath.Join(root, "test.yml"), InputPath: filepath.Join(root, "test.yml"), Content: content, Project: nil}}
		if _, err := testFindings(runTestAnalysis(t.Context(), request)); err != nil {
			t.Fatal(err)
		}
		if !called || rule.Config() != nil {
			t.Fatalf("custom rule config = %#v, want nil; hook called=%v", rule.Config(), called)
		}
	})
}
