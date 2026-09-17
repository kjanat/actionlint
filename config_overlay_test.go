package actionlint

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

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
	var overlays []ConfigOverlay
	for _, in := range []struct{ name, value string }{
		{"config", `{"self-hosted-runner":{"labels":["inline"]},"config-secrets":[],"policy":{"require-commit-hash":false,"require-job-timeout":{"max-minutes":60}}}`},
		{"self-hosted-runner", "labels: [individual]"},
		{"config-variables", "null"},
		{"paths", `"**/*.yaml": {ignore: [added]}`},
		{"policy", "cache-write-untrusted: null"},
	} {
		o, err := ParseConfigOverlay(in.name, []byte(in.value))
		if err != nil {
			t.Fatal(err)
		}
		overlays = append(overlays, o)
	}
	var reports []ConfigReport
	l, err := NewLinter(io.Discard, &LinterOptions{ConfigFile: path, ConfigOverlays: overlays, OnConfigLoaded: func(r ConfigReport) { reports = append(reports, r) }})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := l.configForProject(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.SelfHostedRunner.Labels, []string{"individual"}) {
		t.Errorf("individual input must replace labels: %#v", cfg.SelfHostedRunner)
	}
	if cfg.ConfigVariables != nil || cfg.ConfigSecrets == nil || len(cfg.ConfigSecrets) != 0 {
		t.Errorf("null must disable variable checking, [] must forbid custom secrets: %#v", cfg)
	}
	if cfg.AssumeDefaultPermissions != DefaultPermissionsAssumptionPermissive || cfg.RequiresCommitHash() || cfg.Policy.RequireCommitHash == nil {
		t.Errorf("file settings and explicit false must survive merging: %#v", cfg)
	}
	if !cfg.cachePolicyEnabled("cache-write-untrusted") {
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
	if _, err := l.configForProject(nil); err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || reports[0].File != path || !reports[0].Explicit || len(reports[0].Overrides) != len(overlays) {
		t.Errorf("report selected file and inputs once per project: %#v", reports)
	}
	original, err := ReadConfigFile(path)
	if err != nil || !original.RequiresCommitHash() || original.ConfigVariables[0] != "ORIGINAL" {
		t.Errorf("overlays must leave the config file unchanged: %#v, %v", original, err)
	}
}

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
			base, err := ParseConfigOverlay("config", []byte("policy: {require-commit-hash: true}"))
			if err != nil {
				t.Fatal(err)
			}
			overlay, err := ParseConfigOverlay("config", []byte(tc.value))
			if err != nil {
				t.Fatal(err)
			}
			l, err := NewLinter(io.Discard, &LinterOptions{ConfigOverlays: []ConfigOverlay{base, overlay}})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := l.configForProject(nil)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.RequiresCommitHash() != tc.want {
				t.Errorf("want require-commit-hash=%t after %s", tc.want, tc.value)
			}
		})
	}
}

func TestConfigOverlayRejectsInvalidInputs(t *testing.T) {
	for _, tc := range []struct{ input, value string }{
		{"config", ""},
		{"config", "[broken"},
		{"config", "{}\n---\n{}"},
		{"config", "unknown: true"},
		{"config", "self-hosted-runner: {lables: [typo]}"},
		{"policy", "required-action: [typo]"},
		{"paths", `"**": {ignores: [typo]}`},
		{"config-variables", "["},
		{"config-secrets", "{KEY: value}"},
		{"self-hosted-runner", "[wrong-shape]"},
		{"policy", "require-job-timeout: {min-minutes: 20, max-minutes: 10}"},
		{"assume-default-permissions", "unknown"},
		{"unknown", "null"},
	} {
		t.Run(tc.input+"/"+tc.value, func(t *testing.T) {
			_, err := ParseConfigOverlay(tc.input, []byte(tc.value))
			if err == nil || !strings.Contains(err.Error(), tc.input) {
				t.Errorf("want error naming input %q, got %v", tc.input, err)
			}
		})
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
			overlay, err := ParseConfigOverlay("self-hosted-runner", []byte(tc.overlay))
			if err != nil {
				t.Fatal(err)
			}
			l, err := NewLinter(io.Discard, &LinterOptions{ConfigFile: path, ConfigOverlays: []ConfigOverlay{overlay}})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := l.configForProject(nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg.SelfHostedRunner.Labels, tc.labels) || !reflect.DeepEqual(cfg.ConfigVariables, tc.variables) {
				t.Errorf("unexpected config after overlay: %#v", cfg)
			}
		})
	}
}
