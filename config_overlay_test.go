package actionlint

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestConfigLoadedCallbackCanReenterSession(t *testing.T) {
	var session *AnalysisSession
	var calls int
	var reentered *AnalysisResult
	var reentryErr error
	session, err := NewAnalysisSession(AnalysisOptions{OnConfigLoaded: func(ConfigReport) {
		calls++
		if calls > 1 {
			t.Error("reentry invoked the callback again")
			return
		}
		// Fail without leaving a blocked reentrant call behind when the lock is held.
		if !session.configState.TryLock() {
			t.Error("OnConfigLoaded holds the configuration cache lock")
			return
		}
		session.configState.Unlock()
		reentered, reentryErr = session.ReadStdin(strings.NewReader("on: push\njobs: {}\n"), false)
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.ReadStdin(strings.NewReader("on: push\njobs: {}\n"), false)
	if err != nil || result == nil || result.FileCount() != 1 {
		t.Fatalf("outer analysis: %+v, %v", result, err)
	}
	if reentryErr != nil || reentered == nil || reentered.FileCount() != 1 {
		t.Fatalf("reentrant analysis: %+v, %v", reentered, reentryErr)
	}
	if calls != 1 {
		t.Fatalf("callback invoked %d times for the same project", calls)
	}
}

func TestConfigLoadedCallbackOncePerConcurrentProject(t *testing.T) {
	overlay, err := ParseConfigOverlay("config-variables", []byte("[ALLOWED]"))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	session, err := NewAnalysisSession(AnalysisOptions{
		ConfigOverlays: []ConfigOverlay{overlay},
		OnConfigLoaded: func(ConfigReport) { calls.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	projects := []*Project{{root: "first"}, {root: "second"}}
	type loaded struct {
		project int
		config  *Config
		err     error
	}
	const requests = 16
	results := make(chan loaded, requests)
	start := make(chan struct{})
	var group sync.WaitGroup
	for i := range requests {
		group.Go(func() {
			<-start
			project := i % len(projects)
			config, err := session.configForProject(projects[project])
			results <- loaded{project, config, err}
		})
	}
	close(start)
	group.Wait()
	close(results)
	configs := make(map[int]*Config)
	for result := range results {
		if result.err != nil || result.config == nil || !reflect.DeepEqual(result.config.ConfigVariables, []string{"ALLOWED"}) {
			t.Fatalf("concurrent configuration: %+v", result)
		}
		if previous, ok := configs[result.project]; ok && previous != result.config {
			t.Fatal("concurrent readers received different configurations for the same project")
		}
		configs[result.project] = result.config
	}
	if got := calls.Load(); got != int32(len(projects)) {
		t.Fatalf("callback invoked %d times for %d projects", got, len(projects))
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
		t.Fatalf("report selected file and inputs once per project: %#v", reports)
	}
	inspection := reports[0].Inspection
	for pointer, want := range map[string]ConfigOrigin{
		"/self-hosted-runner/labels":              {Source: "input", Input: "self-hosted-runner", State: "value", Line: 1, Column: 9},
		"/config-variables":                       {Source: "input", Input: "config-variables", State: "null", Line: 1, Column: 1},
		"/assume-default-permissions":             {Source: "config", State: "value", Line: 4, Column: 29},
		"/policy/cache-write-untrusted":           {Source: "input", Input: "policy", State: "null", Line: 1, Column: 24},
		"/policy/require-job-timeout/min-minutes": {Source: "config", State: "value", Line: 8, Column: 38},
		"/policy/cache-operation":                 {Source: "default", State: "missing"},
	} {
		if got := inspection.Origins[pointer]; got != want {
			t.Errorf("%s: got %+v, want %+v", pointer, got, want)
		}
	}
	if inspection.Path != path || inspection.Origins["/policy/require-job-timeout/max-minutes"].Input != "config" {
		t.Errorf("lost file path or whole-config input: %+v", inspection)
	}
	serialized, err := effectiveConfig(cfg)
	if err != nil || !reflect.DeepEqual(inspection.Config, serialized) {
		t.Fatalf("inspection differs from analyzed configuration: %#v, %v", inspection.Config, err)
	}
	original, err := ReadConfigFile(path)
	if err != nil || !original.RequiresCommitHash() || original.ConfigVariables[0] != "ORIGINAL" {
		t.Errorf("overlays must leave the config file unchanged: %#v, %v", original, err)
	}
}

func TestConfigOverlayResetOrigins(t *testing.T) {
	for _, tc := range []struct {
		input, value, source, state string
	}{
		{"config", "null", "input", "null"},
		{"policy", "null", "input", "null"},
		{"policy", "{}", "config", "value"},
	} {
		t.Run(tc.input+"/"+tc.value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("policy: {require-commit-hash: true}"), 0o600); err != nil {
				t.Fatal(err)
			}
			overlay, err := ParseConfigOverlay(tc.input, []byte(tc.value))
			if err != nil {
				t.Fatal(err)
			}
			var report ConfigReport
			session, err := NewAnalysisSession(AnalysisOptions{ConfigFile: path, ConfigOverlays: []ConfigOverlay{overlay}, OnConfigLoaded: func(r ConfigReport) { report = r }})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := session.configForProject(nil)
			if err != nil {
				t.Fatal(err)
			}
			origin := report.Inspection.Origins["/policy/require-commit-hash"]
			if origin.Source != tc.source || origin.State != tc.state || (tc.source == "input" && origin.Input != tc.input) {
				t.Fatalf("reset origin lost: %+v", origin)
			}
			if cfg.RequiresCommitHash() != (tc.value == "{}") {
				t.Fatalf("reset changed analysis semantics: %+v", cfg.Policy)
			}
		})
	}
}

func TestConfigOverlayPartialMappingAfterReset(t *testing.T) {
	for _, reset := range []string{"null", "policy: null"} {
		t.Run(reset, func(t *testing.T) {
			var overlays []ConfigOverlay
			for _, input := range []struct{ name, value string }{
				{"config", reset},
				{"policy", "cache-operation: false"},
				{"policy", "required-actions: []"},
			} {
				overlay, err := ParseConfigOverlay(input.name, []byte(input.value))
				if err != nil {
					t.Fatal(err)
				}
				overlays = append(overlays, overlay)
			}
			var report ConfigReport
			session, err := NewAnalysisSession(AnalysisOptions{ConfigOverlays: overlays, OnConfigLoaded: func(r ConfigReport) { report = r }})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := session.configForProject(nil)
			if err != nil {
				t.Fatal(err)
			}
			origin := report.Inspection.Origins["/policy/require-commit-hash"]
			if origin.Source != "input" || origin.Input != "config" || origin.State != "null" || cfg.RequiresCommitHash() {
				t.Fatalf("lost reset after partial mapping: %+v", report.Inspection)
			}
			origin = report.Inspection.Origins["/policy/cache-operation"]
			if origin.Source != "input" || origin.Input != "policy" || origin.State != "value" || cfg.cachePolicyEnabled("cache-operation") {
				t.Fatalf("lost setting after reset: %+v", report.Inspection)
			}
		})
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
