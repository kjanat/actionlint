package workspace_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestConfigOverlayEmptyAndNullMappings(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"{}", true},
		{"policy: {}", true},
		{"policy: null", false},
		{"null", false},
	} {
		t.Run(tc.value, func(t *testing.T) {
			base, err := repoconfig.ParseOverlay("config", []byte("policy: {require-commit-hash: true}"))
			if err != nil {
				t.Fatal(err)
			}
			overlay, err := repoconfig.ParseOverlay("config", []byte(tc.value))
			if err != nil {
				t.Fatal(err)
			}
			l, err := workspace.New(workspace.Options{ConfigOverlays: []repoconfig.Overlay{base, overlay}})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := l.ConfigForProject(nil)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.RequiresCommitHash() != tc.want {
				t.Errorf("want require-commit-hash=%t after %s", tc.want, tc.value)
			}
		})
	}
}

func TestConfigOverlaysPreserveFileSettings(t *testing.T) {
	base := `self-hosted-runner: {labels: [original]}
config-variables: [ORIGINAL]
config-secrets: [DEPLOY_TOKEN]
assume-default-permissions: permissive
policy:
  cache-write-untrusted: false
  require-commit-hash: true
  require-job-timeout: {min-minutes: 2, max-minutes: 30}
paths:
  "**/*.yml": {ignore: [original]}
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	var overlays []repoconfig.Overlay
	for _, in := range []struct{ name, value string }{
		{"config", `{"self-hosted-runner":{"labels":["inline"]},"config-secrets":[],"policy":{"require-commit-hash":false,"require-job-timeout":{"max-minutes":60}}}`},
		{"self-hosted-runner", "labels: [individual]"},
		{"config-variables", "null"},
		{"paths", `"**/*.yaml": {ignore: [added]}`},
		{"policy", "cache-write-untrusted: null"},
	} {
		o, err := repoconfig.ParseOverlay(in.name, []byte(in.value))
		if err != nil {
			t.Fatal(err)
		}
		overlays = append(overlays, o)
	}
	var reports []repoconfig.Report
	l, err := workspace.New(workspace.Options{ConfigFile: path, ConfigOverlays: overlays, OnConfigLoaded: func(r repoconfig.Report) { reports = append(reports, r) }})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := l.ConfigForProject(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.SelfHostedRunner.Labels, []string{"individual"}) {
		t.Errorf("individual input must replace labels: %#v", cfg.SelfHostedRunner)
	}
	if cfg.ConfigVariables != nil || cfg.ConfigSecrets == nil || len(cfg.ConfigSecrets) != 0 {
		t.Errorf("null must disable variable checking, [] must forbid custom secrets: %#v", cfg)
	}
	if cfg.AssumeDefaultPermissions != repoconfig.DefaultPermissionsAssumptionPermissive || cfg.RequiresCommitHash() || cfg.Policy.RequireCommitHash == nil {
		t.Errorf("file settings and explicit false must survive merging: %#v", cfg)
	}
	if !cfg.CachePolicyEnabled("cache-write-untrusted") {
		t.Error("null must restore the cache policy's enabled default")
	}
	minimum, _ := cfg.RequiresJobTimeout().MinMinutes()
	maximum, _ := cfg.RequiresJobTimeout().MaxMinutes()
	if minimum != 2 || maximum != 60 {
		t.Errorf("partial policy mapping must retain minimum and replace maximum: %v, %v", minimum, maximum)
	}
	if len(cfg.Paths) != 2 || !cfg.Paths["**/*.yml"].Ignore[0].MatchString("original") || !cfg.Paths["**/*.yaml"].Ignore[0].MatchString("added") {
		t.Errorf("path mappings must merge: %#v", cfg.Paths)
	}
	if _, err := l.ConfigForProject(nil); err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || reports[0].File != path || !reports[0].Explicit || len(reports[0].Overrides) != len(overlays) {
		t.Fatalf("report selected file and inputs once per project: %#v", reports)
	}
	inspection := reports[0].Inspection
	for pointer, want := range map[string]repoconfig.Origin{
		"/lint/rules/correctness/runner-label/options/labels":               {Source: "input", Input: "self-hosted-runner", State: "value", Line: 1, Column: 9},
		"/lint/rules/correctness/expression-reference/options/variables":    {Source: "input", Input: "config-variables", State: "null", Line: 1, Column: 1},
		"/lint/rules/correctness/workflow-call/options/default-permissions": {Source: "config", State: "value", Line: 4, Column: 29},
		"/lint/rules/security/cache-write-untrusted/level":                  {Source: "input", Input: "policy", State: "null", Line: 1, Column: 24},
		"/lint/rules/policy/require-job-timeout/options/min-minutes":        {Source: "config", State: "value", Line: 8, Column: 38},
	} {
		if got := inspection.Origins[pointer]; got != want {
			t.Errorf("%s: got %+v, want %+v", pointer, got, want)
		}
	}
	if _, exists := inspection.Origins["/lint/rules/correctness/cache-operation/level"]; exists || !cfg.CachePolicyEnabled("cache-operation") {
		t.Fatalf("unconfigured rule must retain its default without a fabricated origin: %+v", inspection)
	}
	if inspection.Path != path || inspection.Origins["/lint/rules/policy/require-job-timeout/options/max-minutes"].Input != "config" {
		t.Errorf("lost file path or whole-config input: %+v", inspection)
	}
	serialized, err := repoconfig.EffectiveValues(cfg)
	if err != nil || !reflect.DeepEqual(inspection.Config, serialized) {
		t.Fatalf("inspection differs from analyzed configuration: %#v, %v", inspection.Config, err)
	}
	original, err := repoconfig.Load(path)
	if err != nil || !original.RequiresCommitHash() || original.ConfigVariables[0] != "ORIGINAL" {
		t.Errorf("overlays must leave the config file unchanged: %#v, %v", original, err)
	}
}

func TestConfigOverlaysPreserveYAMLAliases(t *testing.T) {
	for _, tc := range []struct {
		name, base, overlay string
		labels, variables   []string
	}{
		{"replaced anchor", "self-hosted-runner: {labels: &names [original]}\nconfig-variables: *names", "labels: [replacement]", []string{"replacement"}, []string{"original"}},
		{"merge key", "<<: {self-hosted-runner: {labels: [original]}}", "{}", []string{"original"}, nil},
		{"merge sequence", "<<: [{self-hosted-runner: {labels: [first]}}, {self-hosted-runner: {labels: [second]}}]", "{}", []string{"first"}, nil},
		{"explicit key wins", "<<: {self-hosted-runner: {labels: [inherited]}}\nself-hosted-runner: {labels: [explicit]}", "{}", []string{"explicit"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.base), 0o600); err != nil {
				t.Fatal(err)
			}
			overlay, err := repoconfig.ParseOverlay("self-hosted-runner", []byte(tc.overlay))
			if err != nil {
				t.Fatal(err)
			}
			l, err := workspace.New(workspace.Options{ConfigFile: path, ConfigOverlays: []repoconfig.Overlay{overlay}})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := l.ConfigForProject(nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg.SelfHostedRunner.Labels, tc.labels) || !reflect.DeepEqual(cfg.ConfigVariables, tc.variables) {
				t.Errorf("unexpected config after overlay: %#v", cfg)
			}
		})
	}
}
